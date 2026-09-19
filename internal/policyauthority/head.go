package policyauthority

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var (
	ErrChainRollback = errors.New("policy authority chain rollback")
	ErrChainFork     = errors.New("policy authority chain fork")
)

type ChainHead struct {
	Protocol           string `json:"protocol"`
	AuthorityID        string `json:"authority_id"`
	Generation         uint64 `json:"generation"`
	SignedManifestHash string `json:"signed_manifest_hash"`
	RotationHash       string `json:"rotation_hash,omitempty"`
	HeadHash           string `json:"head_hash"`
}

type chainHeadMaterial struct {
	Protocol           string `json:"protocol"`
	AuthorityID        string `json:"authority_id"`
	Generation         uint64 `json:"generation"`
	SignedManifestHash string `json:"signed_manifest_hash"`
	RotationHash       string `json:"rotation_hash,omitempty"`
}

func (h ChainHead) material() chainHeadMaterial {
	return chainHeadMaterial{
		Protocol: h.Protocol, AuthorityID: h.AuthorityID, Generation: h.Generation,
		SignedManifestHash: h.SignedManifestHash, RotationHash: h.RotationHash,
	}
}

func (h ChainHead) Validate() error {
	if h.Protocol != ChainHeadProtocol || h.AuthorityID == "" || h.Generation == 0 {
		return errors.New("policy authority chain head fields are incomplete")
	}
	if !shaHex(h.SignedManifestHash) || !shaHex(h.HeadHash) {
		return errors.New("policy authority chain head requires sha256 identifiers")
	}
	if h.RotationHash != "" && !shaHex(h.RotationHash) {
		return errors.New("policy authority chain head rotation_hash must be sha256 hex")
	}
	expected, err := hashJSON(h.material())
	if err != nil {
		return err
	}
	if expected != h.HeadHash {
		return errors.New("policy authority chain head hash mismatch")
	}
	return nil
}

func chainHeadFor(manifests []SignedManifest, rotations []Rotation) (ChainHead, error) {
	if len(manifests) == 0 || len(rotations) != len(manifests)-1 {
		return ChainHead{}, errors.New("invalid policy authority chain for head")
	}
	current := manifests[len(manifests)-1]
	if err := current.SelfVerify(); err != nil {
		return ChainHead{}, err
	}
	h := ChainHead{
		Protocol: ChainHeadProtocol, AuthorityID: current.Manifest.AuthorityID,
		Generation: current.Manifest.Generation, SignedManifestHash: current.SignedManifestHash,
	}
	if len(rotations) > 0 {
		h.RotationHash = rotations[len(rotations)-1].RotationHash
	}
	var err error
	h.HeadHash, err = hashJSON(h.material())
	if err != nil {
		return ChainHead{}, err
	}
	return h, nil
}

func OpenDurableResolver(statePath string, root TrustRoot, manifests []SignedManifest, rotations []Rotation) (*Resolver, error) {
	if statePath == "" {
		return nil, errors.New("policy authority chain-head state path is required")
	}
	r, err := openVerifiedResolver(root, manifests, rotations)
	if err != nil {
		return nil, err
	}
	if err := acceptChainHead(statePath, r.head, manifests, rotations); err != nil {
		return nil, err
	}
	return r, nil
}

func acceptChainHead(path string, candidate ChainHead, manifests []SignedManifest, rotations []Rotation) error {
	if err := candidate.Validate(); err != nil {
		return err
	}
	accepted, err := readChainHead(path)
	if errors.Is(err, os.ErrNotExist) {
		return writeChainHeadAtomic(path, candidate)
	}
	if err != nil {
		return err
	}
	if accepted.AuthorityID != candidate.AuthorityID {
		return fmt.Errorf("%w: authority changed", ErrChainFork)
	}
	if candidate.Generation < accepted.Generation {
		return fmt.Errorf("%w: accepted generation=%d candidate=%d", ErrChainRollback, accepted.Generation, candidate.Generation)
	}
	contained, err := chainContainsHead(accepted, manifests, rotations)
	if err != nil {
		return err
	}
	if !contained {
		return fmt.Errorf("%w: candidate chain does not contain accepted head %s", ErrChainFork, accepted.HeadHash)
	}
	if candidate.Generation == accepted.Generation {
		if candidate.HeadHash != accepted.HeadHash {
			return fmt.Errorf("%w: same generation has different head", ErrChainFork)
		}
		return nil
	}
	return writeChainHeadAtomic(path, candidate)
}

func chainContainsHead(accepted ChainHead, manifests []SignedManifest, rotations []Rotation) (bool, error) {
	if err := accepted.Validate(); err != nil {
		return false, err
	}
	for i, manifest := range manifests {
		if manifest.Manifest.Generation != accepted.Generation {
			continue
		}
		rotationHash := ""
		if i > 0 {
			rotationHash = rotations[i-1].RotationHash
		}
		h := ChainHead{
			Protocol: ChainHeadProtocol, AuthorityID: manifest.Manifest.AuthorityID,
			Generation: manifest.Manifest.Generation, SignedManifestHash: manifest.SignedManifestHash,
			RotationHash: rotationHash,
		}
		var err error
		h.HeadHash, err = hashJSON(h.material())
		if err != nil {
			return false, err
		}
		return h.HeadHash == accepted.HeadHash, nil
	}
	return false, nil
}

func readChainHead(path string) (ChainHead, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return ChainHead{}, err
	}
	var h ChainHead
	if err := json.Unmarshal(b, &h); err != nil {
		return ChainHead{}, fmt.Errorf("invalid policy authority chain-head state: %w", err)
	}
	if err := h.Validate(); err != nil {
		return ChainHead{}, err
	}
	return h, nil
}

func writeChainHeadAtomic(path string, head ChainHead) error {
	if err := head.Validate(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".policy-authority-head-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(head); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

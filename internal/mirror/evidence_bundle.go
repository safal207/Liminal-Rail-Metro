package mirror

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const EvidenceBundleProtocol = "mirror.evidence-bundle.v0.1"

const (
	ArtifactPacket          = "metro.packet"
	ArtifactRoute           = "metro.route"
	ArtifactResult          = "effect.result"
	ArtifactReceipt         = "metro.receipt"
	ArtifactAuthorityPolicy = "mirror.authority-policy"
	ArtifactProofEnvelope   = "mirror.proof-envelope"
	ArtifactReplayReport    = "mirror.replay-report"
)

var requiredBundleArtifacts = []string{
	ArtifactPacket,
	ArtifactRoute,
	ArtifactResult,
	ArtifactReceipt,
	ArtifactAuthorityPolicy,
	ArtifactProofEnvelope,
	ArtifactReplayReport,
}

// ArtifactDigest names one proof input/output by content hash. Ref uses a
// transport-neutral CAS URI; this package defines identity, not storage.
type ArtifactDigest struct {
	Kind          string `json:"kind"`
	HashAlgorithm string `json:"hash_algorithm"`
	Digest        string `json:"digest"`
	Ref           string `json:"ref"`
}

// EvidenceBundle is a compact content-addressed manifest for the artifacts
// required to independently replay a proof.
type EvidenceBundle struct {
	Protocol   string           `json:"protocol"`
	ClaimID    string           `json:"claim_id"`
	ActionID   string           `json:"action_id"`
	Artifacts  []ArtifactDigest `json:"artifacts"`
	BundleHash string           `json:"bundle_hash"`
}

// BuildEvidenceBundle independently replays the proof before emitting a
// manifest. A non-reproducible proof cannot become a bundle.
func BuildEvidenceBundle(
	envelope ProofEnvelope,
	packet metro.Packet,
	route metro.Route,
	result map[string]any,
	receipt metro.Receipt,
	authority AuthorityPolicy,
) (EvidenceBundle, ReplayReport, error) {
	replay, err := (ReplayVerifier{}).Verify(envelope, packet, route, result, receipt, authority)
	if err != nil {
		return EvidenceBundle{}, replay, fmt.Errorf("build evidence bundle replay: %w", err)
	}
	if replay.Status != ReplayStatusReproduced {
		return EvidenceBundle{}, replay, errors.New("evidence bundle requires reproduced replay report")
	}

	packetDigest, err := digestJSON(packet)
	if err != nil {
		return EvidenceBundle{}, replay, fmt.Errorf("hash packet: %w", err)
	}
	routeDigest, err := digestJSON(route)
	if err != nil {
		return EvidenceBundle{}, replay, fmt.Errorf("hash route: %w", err)
	}
	resultDigest, err := digestJSON(result)
	if err != nil {
		return EvidenceBundle{}, replay, fmt.Errorf("hash result: %w", err)
	}
	receiptDigest, err := digestJSON(receipt)
	if err != nil {
		return EvidenceBundle{}, replay, fmt.Errorf("hash receipt: %w", err)
	}
	snapshot, err := authority.Snapshot()
	if err != nil {
		return EvidenceBundle{}, replay, fmt.Errorf("snapshot authority policy: %w", err)
	}
	envelopeDigest, err := digestJSON(envelope)
	if err != nil {
		return EvidenceBundle{}, replay, fmt.Errorf("hash proof envelope: %w", err)
	}
	replayDigest, err := digestJSON(replay)
	if err != nil {
		return EvidenceBundle{}, replay, fmt.Errorf("hash replay report: %w", err)
	}

	bundle := EvidenceBundle{
		Protocol: EvidenceBundleProtocol,
		ClaimID:  envelope.Claim.ID,
		ActionID: envelope.Claim.ActionID,
		Artifacts: []ArtifactDigest{
			newArtifactDigest(ArtifactPacket, packetDigest),
			newArtifactDigest(ArtifactRoute, routeDigest),
			newArtifactDigest(ArtifactResult, resultDigest),
			newArtifactDigest(ArtifactReceipt, receiptDigest),
			newArtifactDigest(ArtifactAuthorityPolicy, snapshot.PolicyHash),
			newArtifactDigest(ArtifactProofEnvelope, envelopeDigest),
			newArtifactDigest(ArtifactReplayReport, replayDigest),
		},
	}

	bundleHash, err := bundle.computeHash()
	if err != nil {
		return EvidenceBundle{}, replay, err
	}
	bundle.BundleHash = bundleHash

	if err := bundle.Validate(); err != nil {
		return EvidenceBundle{}, replay, fmt.Errorf("validate built evidence bundle: %w", err)
	}
	return bundle, replay, nil
}

// Validate checks the manifest itself. It does not fetch content from a CAS.
func (b EvidenceBundle) Validate() error {
	if b.Protocol != EvidenceBundleProtocol {
		return fmt.Errorf("unexpected evidence bundle protocol %q", b.Protocol)
	}
	if b.ClaimID == "" || b.ActionID == "" {
		return errors.New("evidence bundle claim_id and action_id are required")
	}
	if !isSHA256Hex(b.BundleHash) {
		return errors.New("evidence bundle bundle_hash must be sha256 hex")
	}
	if len(b.Artifacts) != len(requiredBundleArtifacts) {
		return fmt.Errorf("evidence bundle requires %d artifacts, got %d", len(requiredBundleArtifacts), len(b.Artifacts))
	}

	seen := make(map[string]bool, len(b.Artifacts))
	for _, artifact := range b.Artifacts {
		if artifact.Kind == "" {
			return errors.New("evidence bundle artifact kind is required")
		}
		if seen[artifact.Kind] {
			return fmt.Errorf("duplicate evidence bundle artifact kind %q", artifact.Kind)
		}
		seen[artifact.Kind] = true
		if artifact.HashAlgorithm != "sha256" {
			return fmt.Errorf("artifact %q uses unsupported hash algorithm %q", artifact.Kind, artifact.HashAlgorithm)
		}
		if !isSHA256Hex(artifact.Digest) {
			return fmt.Errorf("artifact %q digest must be sha256 hex", artifact.Kind)
		}
		if artifact.Ref != casRef(artifact.Digest) {
			return fmt.Errorf("artifact %q ref does not match digest", artifact.Kind)
		}
	}
	for _, kind := range requiredBundleArtifacts {
		if !seen[kind] {
			return fmt.Errorf("missing evidence bundle artifact %q", kind)
		}
	}

	expected, err := b.computeHash()
	if err != nil {
		return err
	}
	if b.BundleHash != expected {
		return errors.New("evidence bundle hash mismatch")
	}
	return nil
}

// VerifyEvidenceBundle independently rebuilds the bundle from original
// artifacts and rejects any digest or root-manifest mismatch.
func VerifyEvidenceBundle(
	bundle EvidenceBundle,
	envelope ProofEnvelope,
	packet metro.Packet,
	route metro.Route,
	result map[string]any,
	receipt metro.Receipt,
	authority AuthorityPolicy,
) (ReplayReport, error) {
	if err := bundle.Validate(); err != nil {
		return ReplayReport{}, fmt.Errorf("validate evidence bundle: %w", err)
	}
	rebuilt, replay, err := BuildEvidenceBundle(envelope, packet, route, result, receipt, authority)
	if err != nil {
		return replay, err
	}
	if bundle.BundleHash != rebuilt.BundleHash {
		return replay, errors.New("evidence bundle root hash does not match supplied artifacts")
	}
	if !sameArtifactSet(bundle.Artifacts, rebuilt.Artifacts) {
		return replay, errors.New("evidence bundle artifact digests do not match supplied artifacts")
	}
	return replay, nil
}

func newArtifactDigest(kind, digest string) ArtifactDigest {
	return ArtifactDigest{
		Kind:          kind,
		HashAlgorithm: "sha256",
		Digest:        digest,
		Ref:           casRef(digest),
	}
}

func casRef(digest string) string {
	return "cas://sha256/" + digest
}

func digestJSON(value any) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func (b EvidenceBundle) computeHash() (string, error) {
	artifacts := append([]ArtifactDigest(nil), b.Artifacts...)
	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].Kind < artifacts[j].Kind
	})
	manifest := struct {
		Protocol  string           `json:"protocol"`
		ClaimID   string           `json:"claim_id"`
		ActionID  string           `json:"action_id"`
		Artifacts []ArtifactDigest `json:"artifacts"`
	}{
		Protocol:  b.Protocol,
		ClaimID:   b.ClaimID,
		ActionID:  b.ActionID,
		Artifacts: artifacts,
	}
	return digestJSON(manifest)
}

func sameArtifactSet(left, right []ArtifactDigest) bool {
	if len(left) != len(right) {
		return false
	}
	byKind := make(map[string]ArtifactDigest, len(left))
	for _, artifact := range left {
		byKind[artifact.Kind] = artifact
	}
	for _, artifact := range right {
		if byKind[artifact.Kind] != artifact {
			return false
		}
	}
	return true
}

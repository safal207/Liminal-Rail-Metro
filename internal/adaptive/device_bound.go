package adaptive

import (
	"crypto/ed25519"
	"errors"
	"fmt"

	"github.com/safal207/Liminal-Rail-Metro/internal/deviceattest"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

// DeviceBoundLearner adds a software-observed device identity and a challenge-
// response attestation layer on top of the v0.5 signed-authority learner.
// The device key is software-held; this is intentionally not a TPM/TEE claim.
type DeviceBoundLearner struct {
	chained    *ChainedAuthorityLearner
	anchor     deviceattest.TrustAnchor
	privateKey ed25519.PrivateKey
}

func EnrollDeviceBoundLearner(path string, chain AuthorityChain, privateKey ed25519.PrivateKey, exploration float64) (*DeviceBoundLearner, deviceattest.TrustAnchor, deviceattest.Evidence, error) {
	current, err := deviceattest.Sense()
	if err != nil {
		return nil, deviceattest.TrustAnchor{}, deviceattest.Evidence{}, err
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, deviceattest.TrustAnchor{}, deviceattest.Evidence{}, errors.New("invalid device attestation private key")
	}
	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok {
		return nil, deviceattest.TrustAnchor{}, deviceattest.Evidence{}, errors.New("invalid device attestation public key")
	}
	anchor, err := deviceattest.NewTrustAnchor(current, publicKey)
	if err != nil {
		return nil, deviceattest.TrustAnchor{}, deviceattest.Evidence{}, err
	}
	learner, err := OpenDeviceBoundLearner(path, chain, anchor, privateKey, exploration)
	if err != nil {
		return nil, deviceattest.TrustAnchor{}, deviceattest.Evidence{}, err
	}
	return learner, anchor, current, nil
}

func OpenDeviceBoundLearner(path string, chain AuthorityChain, anchor deviceattest.TrustAnchor, privateKey ed25519.PrivateKey, exploration float64) (*DeviceBoundLearner, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid device attestation private key")
	}
	current, err := deviceattest.Sense()
	if err != nil {
		return nil, err
	}
	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("invalid device attestation public key")
	}
	if anchor.Protocol != deviceattest.TrustAnchorProtocol || anchor.RootType != deviceattest.RootTypeSoftware {
		return nil, errors.New("unsupported device trust anchor")
	}
	if anchor.DeviceFingerprint != current.DeviceFingerprint {
		return nil, errors.New("current software-observed device does not match pinned device fingerprint")
	}
	if anchor.AttestationKeyID != deviceattest.KeyID(publicKey) {
		return nil, errors.New("device private key does not match pinned attestation key")
	}
	chained, err := OpenChainedAuthorityLearner(path, chain, exploration)
	if err != nil {
		return nil, err
	}
	return &DeviceBoundLearner{chained: chained, anchor: anchor, privateKey: append(ed25519.PrivateKey(nil), privateKey...)}, nil
}

func (l *DeviceBoundLearner) Anchor() deviceattest.TrustAnchor { return l.anchor }
func (l *DeviceBoundLearner) Chain() AuthorityChain            { return l.chained.Chain() }
func (l *DeviceBoundLearner) CurrentSignedGrant() SignedAuthorityGrant {
	return l.chained.CurrentSignedGrant()
}
func (l *DeviceBoundLearner) Choose(ctx Context) (ContextDecision, error) {
	return l.chained.Choose(ctx)
}
func (l *DeviceBoundLearner) SnapshotContext(ctx Context) (map[string]ActionStat, error) {
	return l.chained.SnapshotContext(ctx)
}
func (l *DeviceBoundLearner) JournalHead() (uint64, string) { return l.chained.JournalHead() }

func (l *DeviceBoundLearner) IssueAttestation(nonce, workload, contextKey string) (deviceattest.Attestation, error) {
	current, err := deviceattest.Sense()
	if err != nil {
		return deviceattest.Attestation{}, err
	}
	if current.DeviceFingerprint != l.anchor.DeviceFingerprint {
		return deviceattest.Attestation{}, errors.New("device fingerprint changed since enrollment")
	}
	signed := l.CurrentSignedGrant()
	sequence, head := l.JournalHead()
	return deviceattest.Sign(current, l.privateKey, nonce, signed.SignedAuthorityHash, signed.IssuerKeyID, sequence, head, workload, contextKey)
}

func (l *DeviceBoundLearner) VerifyAttestation(att deviceattest.Attestation, expectedNonce, workload, contextKey string) error {
	current, err := deviceattest.Sense()
	if err != nil {
		return err
	}
	signed := l.CurrentSignedGrant()
	sequence, head := l.JournalHead()
	return deviceattest.VerifyBound(att, l.anchor, expectedNonce, current, signed.SignedAuthorityHash, signed.IssuerKeyID, sequence, head, workload, contextKey)
}

func BindDeviceAttestationResult(result map[string]any, att deviceattest.Attestation) (map[string]any, error) {
	if err := att.SelfVerify(); err != nil {
		return nil, err
	}
	bound := cloneResult(result)
	bound["device_attestation_protocol"] = att.Protocol
	bound["device_root_type"] = att.RootType
	bound["device_fingerprint"] = att.DeviceFingerprint
	bound["runtime_fingerprint"] = att.RuntimeFingerprint
	bound["device_attestation_key_id"] = att.AttestationKeyID
	bound["device_attestation_nonce"] = att.Nonce
	bound["device_attestation_hash"] = att.AttestationHash
	return bound, nil
}

func (l *DeviceBoundLearner) Apply(receipt metro.Receipt, result map[string]any, exp Experience, att deviceattest.Attestation, expectedNonce string) (ApplyResult, error) {
	if err := l.VerifyAttestation(att, expectedNonce, exp.Context.Workload, exp.ContextKey); err != nil {
		return ApplyResult{}, fmt.Errorf("device attestation rejected: %w", err)
	}
	if valueString(result, "device_attestation_protocol") != att.Protocol ||
		valueString(result, "device_root_type") != att.RootType ||
		valueString(result, "device_fingerprint") != att.DeviceFingerprint ||
		valueString(result, "runtime_fingerprint") != att.RuntimeFingerprint ||
		valueString(result, "device_attestation_key_id") != att.AttestationKeyID ||
		valueString(result, "device_attestation_nonce") != att.Nonce ||
		valueString(result, "device_attestation_hash") != att.AttestationHash {
		return ApplyResult{}, errors.New("measured result is not bound to the verified device attestation")
	}
	return l.chained.Apply(receipt, result, exp)
}

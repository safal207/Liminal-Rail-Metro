package adaptive

import (
	"errors"
	"fmt"

	"github.com/safal207/Liminal-Rail-Metro/internal/deviceattest"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

// ProviderBoundLearner is the provider-neutral v0.7 entry point. The adaptive
// runtime depends on the Provider contract, not on software, TPM, TEE, or cloud
// attestation details.
type ProviderBoundLearner struct {
	chained  *ChainedAuthorityLearner
	provider deviceattest.Provider
	anchor   deviceattest.ProviderTrustAnchor
}

func EnrollProviderBoundLearner(path string, chain AuthorityChain, provider deviceattest.Provider, exploration float64) (*ProviderBoundLearner, deviceattest.ProviderTrustAnchor, deviceattest.ProviderEvidence, error) {
	if provider == nil {
		return nil, deviceattest.ProviderTrustAnchor{}, deviceattest.ProviderEvidence{}, errors.New("attestation provider is required")
	}
	anchor, evidence, err := provider.Enroll()
	if err != nil {
		return nil, deviceattest.ProviderTrustAnchor{}, deviceattest.ProviderEvidence{}, err
	}
	learner, err := OpenProviderBoundLearner(path, chain, provider, anchor, exploration)
	if err != nil {
		return nil, deviceattest.ProviderTrustAnchor{}, deviceattest.ProviderEvidence{}, err
	}
	return learner, anchor, evidence, nil
}

func OpenProviderBoundLearner(path string, chain AuthorityChain, provider deviceattest.Provider, anchor deviceattest.ProviderTrustAnchor, exploration float64) (*ProviderBoundLearner, error) {
	if provider == nil {
		return nil, errors.New("attestation provider is required")
	}
	if err := provider.Descriptor().Validate(); err != nil {
		return nil, err
	}
	if err := provider.ValidateAnchor(anchor); err != nil {
		return nil, fmt.Errorf("provider anchor rejected: %w", err)
	}
	chained, err := OpenChainedAuthorityLearner(path, chain, exploration)
	if err != nil {
		return nil, err
	}
	return &ProviderBoundLearner{chained: chained, provider: provider, anchor: anchor}, nil
}

func (l *ProviderBoundLearner) Descriptor() deviceattest.ProviderDescriptor {
	return l.provider.Descriptor()
}

func (l *ProviderBoundLearner) Anchor() deviceattest.ProviderTrustAnchor { return l.anchor }
func (l *ProviderBoundLearner) Chain() AuthorityChain                    { return l.chained.Chain() }
func (l *ProviderBoundLearner) CurrentSignedGrant() SignedAuthorityGrant {
	return l.chained.CurrentSignedGrant()
}
func (l *ProviderBoundLearner) Choose(ctx Context) (ContextDecision, error) {
	return l.chained.Choose(ctx)
}
func (l *ProviderBoundLearner) SnapshotContext(ctx Context) (map[string]ActionStat, error) {
	return l.chained.SnapshotContext(ctx)
}
func (l *ProviderBoundLearner) JournalHead() (uint64, string) { return l.chained.JournalHead() }

func (l *ProviderBoundLearner) IssueAttestation(nonce, workload, contextKey string) (deviceattest.ProviderAttestation, deviceattest.ProviderBinding, error) {
	signed := l.CurrentSignedGrant()
	sequence, head := l.JournalHead()
	binding := deviceattest.ProviderBinding{
		Nonce:                nonce,
		SignedAuthorityHash:  signed.SignedAuthorityHash,
		AuthorityIssuerKeyID: signed.IssuerKeyID,
		JournalSequence:      sequence,
		JournalHead:          head,
		Workload:             workload,
		ContextKey:           contextKey,
	}
	att, err := l.provider.Attest(binding)
	if err != nil {
		return deviceattest.ProviderAttestation{}, deviceattest.ProviderBinding{}, err
	}
	if err := l.provider.Verify(att, l.anchor, binding); err != nil {
		return deviceattest.ProviderAttestation{}, deviceattest.ProviderBinding{}, fmt.Errorf("provider self-check failed: %w", err)
	}
	return att, binding, nil
}

func BindProviderAttestationResult(result map[string]any, att deviceattest.ProviderAttestation, anchor deviceattest.ProviderTrustAnchor) (map[string]any, error) {
	if att.Protocol != deviceattest.ProviderAttestationProtocol || anchor.Protocol != deviceattest.ProviderAnchorProtocol {
		return nil, errors.New("unsupported provider attestation/anchor protocol")
	}
	if att.Descriptor != anchor.Descriptor || att.DescriptorHash != anchor.DescriptorHash {
		return nil, errors.New("provider attestation and anchor descriptor mismatch")
	}
	descriptorHash, err := att.Descriptor.Hash()
	if err != nil {
		return nil, err
	}
	if descriptorHash != att.DescriptorHash {
		return nil, errors.New("provider descriptor hash mismatch")
	}
	bound := cloneResult(result)
	bound["attestation_provider_protocol"] = att.Descriptor.Protocol
	bound["attestation_provider_id"] = att.Descriptor.ProviderID
	bound["attestation_root_type"] = att.Descriptor.RootType
	bound["attestation_assurance_level"] = att.Descriptor.AssuranceLevel
	bound["attestation_hardware_backed"] = att.Descriptor.HardwareBacked
	bound["attestation_remote_verifiable"] = att.Descriptor.RemoteVerifiable
	bound["attestation_descriptor_hash"] = att.DescriptorHash
	bound["provider_anchor_hash"] = anchor.AnchorHash
	bound["provider_evidence_hash"] = att.EvidenceHash
	bound["provider_attestation_hash"] = att.AttestationHash
	return bound, nil
}

func (l *ProviderBoundLearner) Apply(receipt metro.Receipt, result map[string]any, exp Experience, att deviceattest.ProviderAttestation, binding deviceattest.ProviderBinding) (ApplyResult, error) {
	signed := l.CurrentSignedGrant()
	sequence, head := l.JournalHead()
	expected := deviceattest.ProviderBinding{
		Nonce:                binding.Nonce,
		SignedAuthorityHash:  signed.SignedAuthorityHash,
		AuthorityIssuerKeyID: signed.IssuerKeyID,
		JournalSequence:      sequence,
		JournalHead:          head,
		Workload:             exp.Context.Workload,
		ContextKey:           exp.ContextKey,
	}
	if binding != expected {
		return ApplyResult{}, errors.New("attestation provider binding does not match current authority/journal/context")
	}
	if err := l.provider.Verify(att, l.anchor, binding); err != nil {
		return ApplyResult{}, fmt.Errorf("provider attestation rejected: %w", err)
	}
	descriptor := l.provider.Descriptor()
	if valueString(result, "attestation_provider_protocol") != descriptor.Protocol ||
		valueString(result, "attestation_provider_id") != descriptor.ProviderID ||
		valueString(result, "attestation_root_type") != descriptor.RootType ||
		valueString(result, "attestation_assurance_level") != descriptor.AssuranceLevel ||
		valueString(result, "attestation_descriptor_hash") != att.DescriptorHash ||
		valueString(result, "provider_anchor_hash") != l.anchor.AnchorHash ||
		valueString(result, "provider_evidence_hash") != att.EvidenceHash ||
		valueString(result, "provider_attestation_hash") != att.AttestationHash {
		return ApplyResult{}, errors.New("measured result is not bound to verified provider attestation")
	}
	hardwareBacked, ok := result["attestation_hardware_backed"].(bool)
	if !ok || hardwareBacked != descriptor.HardwareBacked {
		return ApplyResult{}, errors.New("measured result hardware-backed capability mismatch")
	}
	remoteVerifiable, ok := result["attestation_remote_verifiable"].(bool)
	if !ok || remoteVerifiable != descriptor.RemoteVerifiable {
		return ApplyResult{}, errors.New("measured result remote-verifiable capability mismatch")
	}
	return l.chained.Apply(receipt, result, exp)
}

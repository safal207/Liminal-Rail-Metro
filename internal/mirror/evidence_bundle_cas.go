package mirror

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

// StoreEvidenceBundle builds a reproducible evidence bundle and persists every
// referenced artifact into the provided content-addressed store. The store's
// computed refs must match the bundle digests exactly.
func StoreEvidenceBundle(
	store CASStore,
	envelope ProofEnvelope,
	packet metro.Packet,
	route metro.Route,
	result map[string]any,
	receipt metro.Receipt,
	authority AuthorityPolicy,
) (EvidenceBundle, ReplayReport, error) {
	if store == nil {
		return EvidenceBundle{}, ReplayReport{}, errors.New("CAS store is required")
	}

	bundle, replay, err := BuildEvidenceBundle(envelope, packet, route, result, receipt, authority)
	if err != nil {
		return EvidenceBundle{}, replay, err
	}

	canonicalPolicy, err := authority.CanonicalJSON()
	if err != nil {
		return EvidenceBundle{}, replay, fmt.Errorf("canonicalize authority policy: %w", err)
	}

	artifacts := map[string][]byte{}
	if artifacts[ArtifactPacket], err = json.Marshal(packet); err != nil {
		return EvidenceBundle{}, replay, fmt.Errorf("marshal packet: %w", err)
	}
	if artifacts[ArtifactRoute], err = json.Marshal(route); err != nil {
		return EvidenceBundle{}, replay, fmt.Errorf("marshal route: %w", err)
	}
	if artifacts[ArtifactResult], err = json.Marshal(result); err != nil {
		return EvidenceBundle{}, replay, fmt.Errorf("marshal result: %w", err)
	}
	if artifacts[ArtifactReceipt], err = json.Marshal(receipt); err != nil {
		return EvidenceBundle{}, replay, fmt.Errorf("marshal receipt: %w", err)
	}
	artifacts[ArtifactAuthorityPolicy] = canonicalPolicy
	if artifacts[ArtifactProofEnvelope], err = json.Marshal(envelope); err != nil {
		return EvidenceBundle{}, replay, fmt.Errorf("marshal proof envelope: %w", err)
	}
	if artifacts[ArtifactReplayReport], err = json.Marshal(replay); err != nil {
		return EvidenceBundle{}, replay, fmt.Errorf("marshal replay report: %w", err)
	}

	for _, entry := range bundle.Artifacts {
		content, ok := artifacts[entry.Kind]
		if !ok {
			return EvidenceBundle{}, replay, fmt.Errorf("missing materialized artifact %q", entry.Kind)
		}
		ref, putErr := store.Put(content)
		if putErr != nil {
			return EvidenceBundle{}, replay, fmt.Errorf("store artifact %q: %w", entry.Kind, putErr)
		}
		if ref != entry.Ref {
			return EvidenceBundle{}, replay, fmt.Errorf("CAS ref mismatch for %q: manifest=%s store=%s", entry.Kind, entry.Ref, ref)
		}
	}

	return bundle, replay, nil
}

// VerifyEvidenceBundleFromCAS resolves every artifact solely from bundle refs,
// verifies each raw byte digest before decoding the exact inputs, and
// independently replays the proof. No caller-provided in-memory source
// artifacts are trusted.
func VerifyEvidenceBundleFromCAS(bundle EvidenceBundle, resolver CASResolver) (ReplayReport, error) {
	if resolver == nil {
		return ReplayReport{}, errors.New("CAS resolver is required")
	}
	if err := bundle.Validate(); err != nil {
		return ReplayReport{}, fmt.Errorf("validate evidence bundle: %w", err)
	}

	resolved := make(map[string][]byte, len(bundle.Artifacts))
	for _, entry := range bundle.Artifacts {
		content, err := resolver.Resolve(entry.Ref)
		if err != nil {
			return ReplayReport{}, fmt.Errorf("resolve %q: %w", entry.Kind, err)
		}
		digest := digestBytes(content)
		if digest != entry.Digest {
			return ReplayReport{}, fmt.Errorf("resolved artifact %q digest mismatch", entry.Kind)
		}
		resolved[entry.Kind] = content
	}

	var packet metro.Packet
	if err := json.Unmarshal(resolved[ArtifactPacket], &packet); err != nil {
		return ReplayReport{}, fmt.Errorf("decode packet: %w", err)
	}
	var route metro.Route
	if err := json.Unmarshal(resolved[ArtifactRoute], &route); err != nil {
		return ReplayReport{}, fmt.Errorf("decode route: %w", err)
	}
	var result map[string]any
	if err := json.Unmarshal(resolved[ArtifactResult], &result); err != nil {
		return ReplayReport{}, fmt.Errorf("decode result: %w", err)
	}
	var receipt metro.Receipt
	if err := json.Unmarshal(resolved[ArtifactReceipt], &receipt); err != nil {
		return ReplayReport{}, fmt.Errorf("decode receipt: %w", err)
	}
	var authority AuthorityPolicy
	if err := json.Unmarshal(resolved[ArtifactAuthorityPolicy], &authority); err != nil {
		return ReplayReport{}, fmt.Errorf("decode authority policy: %w", err)
	}
	var envelope ProofEnvelope
	if err := json.Unmarshal(resolved[ArtifactProofEnvelope], &envelope); err != nil {
		return ReplayReport{}, fmt.Errorf("decode proof envelope: %w", err)
	}
	var reportedReplay ReplayReport
	if err := json.Unmarshal(resolved[ArtifactReplayReport], &reportedReplay); err != nil {
		return ReplayReport{}, fmt.Errorf("decode replay report: %w", err)
	}

	replayed, err := VerifyEvidenceBundle(bundle, envelope, packet, route, result, receipt, authority)
	if err != nil {
		return replayed, err
	}
	if reportedReplay.Status != ReplayStatusReproduced {
		return replayed, errors.New("stored replay report is not REPRODUCED")
	}
	if reportedDigest, err := digestJSON(reportedReplay); err != nil {
		return replayed, fmt.Errorf("hash reported replay: %w", err)
	} else if artifactDigest(bundle, ArtifactReplayReport) != reportedDigest {
		return replayed, errors.New("stored replay report does not match bundle digest")
	}
	return replayed, nil
}

func digestBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func artifactDigest(bundle EvidenceBundle, kind string) string {
	for _, artifact := range bundle.Artifacts {
		if artifact.Kind == kind {
			return artifact.Digest
		}
	}
	return ""
}

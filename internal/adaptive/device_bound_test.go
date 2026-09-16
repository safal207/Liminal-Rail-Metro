package adaptive

import (
	"crypto/ed25519"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/deviceattest"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func deterministicDeviceKey(label string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte(label))
	return ed25519.NewKeyFromSeed(seed[:])
}

func deviceTestChain(t *testing.T) AuthorityChain {
	t.Helper()
	grant, err := NewAuthorityGrant("device-test-authority", "epoch-001", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := SignAuthorityGrant(grant, "device-test-issuer", deterministicDeviceKey("authority-key"))
	if err != nil {
		t.Fatal(err)
	}
	chain, err := NewRootAuthorityChain(signed)
	if err != nil {
		t.Fatal(err)
	}
	return chain
}

func deviceMeasurement(t *testing.T, learner *DeviceBoundLearner, ctx Context, id, nonce string, reward float64) (metro.Receipt, map[string]any, Experience, deviceattest.Attestation) {
	t.Helper()
	decision, err := learner.Choose(ctx)
	if err != nil {
		t.Fatal(err)
	}
	att, err := learner.IssueAttestation(nonce, ctx.Workload, decision.ContextKey)
	if err != nil {
		t.Fatal(err)
	}
	signed := learner.CurrentSignedGrant()
	baseResult := map[string]any{"selected_action": decision.Decision.Action, "context_key": decision.ContextKey, "reward": reward, "reward_unit": "unit"}
	result, err := BindSignedAuthorityResult(baseResult, signed)
	if err != nil {
		t.Fatal(err)
	}
	result, err = BindDeviceAttestationResult(result, att)
	if err != nil {
		t.Fatal(err)
	}
	packet := metro.NewPacket(id, "device-test", "test device-bound learning", metro.Action{Kind: "hardware.test", Inputs: map[string]any{"context_key": decision.ContextKey}}, signed.Grant.AllowedActions)
	route := metro.Route{Protocol: metro.RouteProtocol, RouteID: "route-" + id, ActionID: id, RouterID: "device-test", DecisionMode: "device-bound", SelectedTarget: decision.Decision.Action, DecidedAt: metro.NowISO()}
	experienceID := "experience-" + id
	receipt, err := metro.MakeSuccessReceipt(packet, route, result, "adaptive-experience://"+experienceID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := PredictStatsAfter(decision.Decision.Stats, decision.Decision.Action, reward)
	if err != nil {
		t.Fatal(err)
	}
	exp, err := NewExperience(ExperienceInput{ExperienceID: experienceID, ActionID: id, RouteID: route.RouteID, ReceiptID: receipt.ReceiptID, ReceiptResultHash: receipt.ResultHash, Context: ctx, SelectedAction: decision.Decision.Action, Reward: reward, RewardUnit: "unit", PolicyStatsBefore: decision.Decision.Stats, PolicyStatsAfter: after})
	if err != nil {
		t.Fatal(err)
	}
	return receipt, result, exp, att
}

func TestDeviceBoundLearnerPersistsAndRejectsStaleAttestation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	chain := deviceTestChain(t)
	key := deterministicDeviceKey("device-key")
	learner, anchor, _, err := EnrollDeviceBoundLearner(path, chain, key, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	ctx := fixedContext(t)
	receipt, result, exp, att := deviceMeasurement(t, learner, ctx, "action-device-001", "nonce-001", 10)
	if _, err := learner.Apply(receipt, result, exp, att, "nonce-001"); err != nil {
		t.Fatal(err)
	}
	before, err := learner.SnapshotContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := learner.VerifyAttestation(att, "nonce-001", ctx.Workload, exp.ContextKey); err == nil {
		t.Fatal("expected stale attestation journal anchor to be rejected after apply")
	}
	reopened, err := OpenDeviceBoundLearner(path, chain, anchor, key, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	after, err := reopened.SnapshotContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !sameStats(before, after) {
		t.Fatal("device-bound learner did not restore identical policy state")
	}
}

func TestDeviceBoundLearnerRejectsWrongPinnedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	chain := deviceTestChain(t)
	good := deterministicDeviceKey("device-good")
	_, anchor, _, err := EnrollDeviceBoundLearner(path, chain, good, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDeviceBoundLearner(path, chain, anchor, deterministicDeviceKey("device-other"), 0.5); err == nil {
		t.Fatal("expected wrong software device key to be rejected")
	}
}

func TestDeviceBoundLearnerRejectsResultNotBoundToAttestation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	chain := deviceTestChain(t)
	key := deterministicDeviceKey("device-key")
	learner, _, _, err := EnrollDeviceBoundLearner(path, chain, key, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	ctx := fixedContext(t)
	receipt, result, exp, att := deviceMeasurement(t, learner, ctx, "action-device-002", "nonce-002", 10)
	result["device_attestation_hash"] = "forged"
	if _, err := learner.Apply(receipt, result, exp, att, "nonce-002"); err == nil {
		t.Fatal("expected result without exact device attestation binding to be rejected")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("rejected device evidence should not create journal, stat err=%v", err)
	}
}

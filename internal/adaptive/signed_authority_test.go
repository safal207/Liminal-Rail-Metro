package adaptive

import (
	"crypto/ed25519"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func signedTestPrivateKey(label string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("liminal-test-key:" + label))
	return ed25519.NewKeyFromSeed(seed[:])
}

func signedMeasurement(t *testing.T, learner *ChainedAuthorityLearner, ctx Context, id string, reward float64) (metro.Receipt, map[string]any, Experience) {
	t.Helper()
	decision, err := learner.Choose(ctx)
	if err != nil {
		t.Fatal(err)
	}
	signed := learner.CurrentSignedGrant()
	action := decision.Decision.Action
	baseResult := map[string]any{"selected_action": action, "context_key": decision.ContextKey, "reward": reward, "reward_unit": "unit"}
	result, err := BindSignedAuthorityResult(baseResult, signed)
	if err != nil {
		t.Fatal(err)
	}
	packet := metro.NewPacket(id, "signed-test", "signed-test", metro.Action{Kind: "hardware.test", Inputs: map[string]any{"context_key": decision.ContextKey}}, signed.Grant.AllowedActions)
	route := metro.Route{Protocol: metro.RouteProtocol, RouteID: "route-" + id, ActionID: id, RouterID: "signed-authority-test", DecisionMode: "signed-authority-adaptive", SelectedTarget: action, DecidedAt: metro.NowISO()}
	expID := "experience-" + id
	receipt, err := metro.MakeSuccessReceipt(packet, route, result, "adaptive-experience://"+expID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := PredictStatsAfter(decision.Decision.Stats, action, reward)
	if err != nil {
		t.Fatal(err)
	}
	exp, err := NewExperience(ExperienceInput{ExperienceID: expID, ActionID: id, RouteID: route.RouteID, ReceiptID: receipt.ReceiptID, ReceiptResultHash: receipt.ResultHash, Context: ctx, SelectedAction: action, Reward: reward, RewardUnit: "unit", PolicyStatsBefore: decision.Decision.Stats, PolicyStatsAfter: after})
	if err != nil {
		t.Fatal(err)
	}
	return receipt, result, exp
}

func signedRoot(t *testing.T, actions []string) (SignedAuthorityGrant, ed25519.PrivateKey, AuthorityChain) {
	t.Helper()
	grant, err := NewAuthorityGrant("hardware-local", "epoch-001", actions)
	if err != nil {
		t.Fatal(err)
	}
	privateKey := signedTestPrivateKey("issuer-root")
	signed, err := SignAuthorityGrant(grant, "issuer-root", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := NewRootAuthorityChain(signed)
	if err != nil {
		t.Fatal(err)
	}
	return signed, privateKey, chain
}

func TestSignedAuthorityRejectsTampering(t *testing.T) {
	signed, _, chain := signedRoot(t, []string{"a", "b"})
	if err := signed.SelfVerify(); err != nil {
		t.Fatal(err)
	}
	if err := chain.Verify(); err != nil {
		t.Fatal(err)
	}

	tamperedIssuer := signed
	tamperedIssuer.IssuerID = "attacker"
	if err := tamperedIssuer.SelfVerify(); err == nil {
		t.Fatal("expected issuer tampering to invalidate signature")
	}

	tamperedGrant := signed
	tamperedGrant.Grant.Epoch = "epoch-evil"
	if err := tamperedGrant.SelfVerify(); err == nil {
		t.Fatal("expected grant tampering to invalidate signed authority")
	}

	tamperedRoot := chain
	tamperedRoot.Root.IssuerKeyID = string(make([]byte, 64))
	if err := tamperedRoot.Verify(); err == nil {
		t.Fatal("expected trust-root tampering to fail")
	}
}

func TestAuthorityChainRequiresExplicitSignedRotation(t *testing.T) {
	_, _, chain := signedRoot(t, []string{"a", "b"})
	nextGrant, err := NewAuthorityGrant("hardware-local", "epoch-002", []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	next, err := SignAuthorityGrant(nextGrant, "issuer-next", signedTestPrivateKey("issuer-next"))
	if err != nil {
		t.Fatal(err)
	}

	withoutRotation := cloneAuthorityChain(chain)
	withoutRotation.Grants = append(withoutRotation.Grants, next)
	if err := withoutRotation.Verify(); err == nil {
		t.Fatal("expected epoch transition without rotation record to fail")
	}
}

func TestSignedRotationAuthorizesNewKeyAndAction(t *testing.T) {
	_, rootPrivate, chain := signedRoot(t, []string{"a", "b"})
	path1 := filepath.Join(t.TempDir(), "epoch1.jsonl")
	path2 := filepath.Join(t.TempDir(), "epoch2.jsonl")
	ctx := fixedContext(t)
	learner, err := OpenChainedAuthorityLearner(path1, chain, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	receipt, result, exp := signedMeasurement(t, learner, ctx, "action-001", 10)
	if _, err := learner.Apply(receipt, result, exp); err != nil {
		t.Fatal(err)
	}
	seq, head := learner.JournalHead()
	if seq != 1 || head == "" {
		t.Fatalf("unexpected source journal anchor: seq=%d head=%q", seq, head)
	}

	nextGrant, err := NewAuthorityGrant("hardware-local", "epoch-002", []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	nextPrivate := signedTestPrivateKey("issuer-next")
	next, err := SignAuthorityGrant(nextGrant, "issuer-next", nextPrivate)
	if err != nil {
		t.Fatal(err)
	}

	rotated, rotation, err := learner.Rotate(next, path2, rootPrivate, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if rotation.SourceJournalSequence != seq || rotation.SourceJournalHead != head {
		t.Fatal("rotation was not anchored to the actual source journal head")
	}
	if err := rotated.Chain().Verify(); err != nil {
		t.Fatal(err)
	}
	if !rotated.CurrentSignedGrant().Grant.Allows("c") {
		t.Fatal("explicitly rotated authority did not expose newly authorized action")
	}
	if rotated.CurrentSignedGrant().IssuerKeyID == learner.CurrentSignedGrant().IssuerKeyID {
		t.Fatal("test expected issuer key rotation")
	}

	reopened, err := OpenChainedAuthorityLearner(path2, rotated.Chain(), 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.CurrentSignedGrant().Grant.Allows("c") {
		t.Fatal("verified authority chain was not reusable after restart")
	}

	tamperedChain := rotated.Chain()
	tamperedChain.Rotations[0].ToIssuerKeyID = learner.CurrentSignedGrant().IssuerKeyID
	if err := tamperedChain.Verify(); err == nil {
		t.Fatal("expected tampered rotation target key to fail verification")
	}

	if _, _, err := learner.Rotate(next, path1, rootPrivate, 0.5); err == nil {
		t.Fatal("expected authority rotation to require a distinct journal")
	}
}

func TestSignedAuthorityEvidenceFailsBeforeJournalMutation(t *testing.T) {
	_, _, chain := signedRoot(t, []string{"a", "b"})
	path := filepath.Join(t.TempDir(), "signed.jsonl")
	learner, err := OpenChainedAuthorityLearner(path, chain, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	ctx := fixedContext(t)
	receipt, result, exp := signedMeasurement(t, learner, ctx, "action-signed-tamper", 10)
	result["signed_authority_hash"] = "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := learner.Apply(receipt, result, exp); err == nil {
		t.Fatal("expected signed authority mismatch to fail closed")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("rejected signed evidence should not create journal, stat err=%v", err)
	}
}

func TestV04JournalCannotSilentlyUpgradeToSignedAuthority(t *testing.T) {
	signed, _, chain := signedRoot(t, []string{"a", "b"})
	path := filepath.Join(t.TempDir(), "legacy-v04.jsonl")
	legacy, err := OpenAuthorityLearner(path, signed.Grant, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	ctx := fixedContext(t)
	receipt, result, exp := authorityMeasurement(t, legacy, ctx, "legacy-action", 10)
	if _, err := legacy.Apply(receipt, result, exp); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenChainedAuthorityLearner(path, chain, 0.5); err == nil {
		t.Fatal("expected unsigned v0.4 journal evidence to be rejected by v0.5 entry point")
	}
}

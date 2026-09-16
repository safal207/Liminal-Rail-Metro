package adaptive

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func authorityMeasurement(t *testing.T, learner *AuthorityLearner, ctx Context, id string, reward float64) (metro.Receipt, map[string]any, Experience) {
	t.Helper()
	decision, err := learner.Choose(ctx)
	if err != nil {
		t.Fatal(err)
	}
	grant := learner.Grant()
	action := decision.Decision.Action
	baseResult := map[string]any{"selected_action": action, "context_key": decision.ContextKey, "reward": reward, "reward_unit": "unit"}
	result, err := BindAuthorityResult(baseResult, grant)
	if err != nil {
		t.Fatal(err)
	}
	packet := metro.NewPacket(id, "test", "test", metro.Action{Kind: "hardware.test", Inputs: map[string]any{"context_key": decision.ContextKey}}, grant.AllowedActions)
	route := metro.Route{Protocol: metro.RouteProtocol, RouteID: "route-" + id, ActionID: id, RouterID: "authority-test", DecisionMode: "authority-adaptive", SelectedTarget: action, DecidedAt: metro.NowISO()}
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

func TestAuthorityGrantHashIsCanonicalAcrossInputOrder(t *testing.T) {
	a, err := NewAuthorityGrant("hardware-local", "epoch-001", []string{"b", "a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewAuthorityGrant("hardware-local", "epoch-001", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if a.AuthorityHash != b.AuthorityHash {
		t.Fatal("equivalent allow-lists produced different authority hashes")
	}
	if a.AllowedActions[0] != "a" || a.AllowedActions[1] != "b" {
		t.Fatalf("allow-list not canonical: %#v", a.AllowedActions)
	}
}

func TestAuthorityLearnerRestartRequiresSameEpochAndAllowList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	ctx := fixedContext(t)
	grant, err := NewAuthorityGrant("hardware-local", "epoch-001", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	l, err := OpenAuthorityLearner(path, grant, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	receipt, result, exp := authorityMeasurement(t, l, ctx, "action-001", 10)
	if _, err := l.Apply(receipt, result, exp); err != nil {
		t.Fatal(err)
	}
	before, err := l.SnapshotContext(ctx)
	if err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenAuthorityLearner(path, grant, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	after, err := reopened.SnapshotContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !sameStats(before, after) {
		t.Fatal("same authority epoch did not restore identical policy state")
	}

	expanded, err := NewAuthorityGrant("hardware-local", "epoch-001", []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenAuthorityLearner(path, expanded, 0.5); err == nil {
		t.Fatal("expected silent same-epoch authority expansion to fail closed")
	}

	rotated, err := NewAuthorityGrant("hardware-local", "epoch-002", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenAuthorityLearner(path, rotated, 0.5); err == nil {
		t.Fatal("expected a different authority epoch to require an explicit new journal/migration")
	}
}

func TestAuthorityLearnerRejectsUnauthorizedRewardBeforeJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	ctx := fixedContext(t)
	grant, err := NewAuthorityGrant("hardware-local", "epoch-001", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	l, err := OpenAuthorityLearner(path, grant, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	receipt, result, exp := authorityMeasurement(t, l, ctx, "action-unauthorized", 10)
	exp.SelectedAction = "c"
	result["selected_action"] = "c"
	if _, err := l.Apply(receipt, result, exp); err == nil {
		t.Fatal("expected action outside authority allow-list to be rejected")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unauthorized evidence should not create journal, stat err=%v", err)
	}
}

func TestAuthorityLearnerRejectsMismatchedEpochBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	ctx := fixedContext(t)
	grant, err := NewAuthorityGrant("hardware-local", "epoch-001", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	l, err := OpenAuthorityLearner(path, grant, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	receipt, result, exp := authorityMeasurement(t, l, ctx, "action-epoch-mismatch", 10)
	result["authority_epoch"] = "epoch-evil"
	if _, err := l.Apply(receipt, result, exp); err == nil {
		t.Fatal("expected mismatched authority epoch to be rejected")
	}
}

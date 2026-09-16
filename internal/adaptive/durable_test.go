package adaptive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func fixedContext(t *testing.T) Context {
	t.Helper()
	c := Context{Workload: "test", CPUClass: "3-4", LoadClass: "low", MemoryClass: "medium"}
	if _, err := c.Key(); err != nil {
		t.Fatal(err)
	}
	return c
}

func measurement(t *testing.T, learner *DurableLearner, ctx Context, id string, reward float64) (metro.Receipt, map[string]any, Experience) {
	t.Helper()
	decision, err := learner.Choose(ctx)
	if err != nil {
		t.Fatal(err)
	}
	action := decision.Decision.Action
	key := decision.ContextKey
	packet := metro.NewPacket(id, "test", "test", metro.Action{Kind: "hardware.test", Inputs: map[string]any{"context_key": key}}, []string{"a", "b"})
	route := metro.Route{Protocol: metro.RouteProtocol, RouteID: "route-" + id, ActionID: id, RouterID: "test", DecisionMode: "adaptive", SelectedTarget: action, DecidedAt: metro.NowISO()}
	result := map[string]any{"selected_action": action, "context_key": key, "reward": reward, "reward_unit": "unit"}
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

func TestDurableLearnerRestartAndDuplicateAreReplaySafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	ctx := fixedContext(t)
	l, err := OpenDurableLearner(path, []string{"a", "b"}, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	receipt, result, exp := measurement(t, l, ctx, "action-001", 10)
	applied, err := l.Apply(receipt, result, exp)
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Applied || applied.Duplicate {
		t.Fatalf("unexpected apply result: %+v", applied)
	}
	beforeRestart, err := l.SnapshotContext(ctx)
	if err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenDurableLearner(path, []string{"a", "b"}, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	afterRestart, err := reopened.SnapshotContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !sameStats(beforeRestart, afterRestart) {
		t.Fatal("replayed state changed across restart")
	}

	dup, err := reopened.Apply(receipt, result, exp)
	if err != nil {
		t.Fatal(err)
	}
	if dup.Applied || !dup.Duplicate {
		t.Fatalf("expected idempotent duplicate, got %+v", dup)
	}
	afterDup, err := reopened.SnapshotContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !sameStats(afterRestart, afterDup) {
		t.Fatal("duplicate reward changed policy")
	}
}

func TestDurableLearnerRejectsUnknownReceiptWithoutLearning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	ctx := fixedContext(t)
	l, err := OpenDurableLearner(path, []string{"a", "b"}, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	receipt, result, exp := measurement(t, l, ctx, "action-unknown", 10)
	receipt.Status = "UNKNOWN"
	if _, err := l.Apply(receipt, result, exp); err == nil {
		t.Fatal("expected UNKNOWN receipt to be rejected")
	}
	snap, err := l.SnapshotContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snap[exp.SelectedAction].Count != 0 {
		t.Fatal("unverified receipt changed policy")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("rejected evidence should not create journal, stat err=%v", err)
	}
}

func TestDurableLearnerRejectsTamperedResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	ctx := fixedContext(t)
	l, err := OpenDurableLearner(path, []string{"a", "b"}, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	receipt, result, exp := measurement(t, l, ctx, "action-tamper", 10)
	result["reward"] = 999.0
	if _, err := l.Apply(receipt, result, exp); err == nil {
		t.Fatal("expected result hash mismatch")
	}
}

func TestDurableLearnerDetectsJournalTampering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	ctx := fixedContext(t)
	l, err := OpenDurableLearner(path, []string{"a", "b"}, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	receipt, result, exp := measurement(t, l, ctx, "action-tamper-journal", 10)
	if _, err := l.Apply(receipt, result, exp); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(b), `"reward":10`, `"reward":11`, 1)
	if tampered == string(b) {
		t.Fatal("test did not modify journal")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDurableLearner(path, []string{"a", "b"}, 0.5); err == nil {
		t.Fatal("expected tampered journal to fail closed")
	}
}

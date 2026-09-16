package adaptive

import (
	"strings"
	"testing"
)

func TestExperienceRequiresEvidenceAdvance(t *testing.T) {
	ctx := Context{Workload: "sha256", CPUClass: "3-4", LoadClass: "low", MemoryClass: "high"}
	before := map[string]ActionStat{"workers=4": {Count: 2, MeanReward: 10}}
	after := map[string]ActionStat{"workers=4": {Count: 3, MeanReward: 11}}
	e, err := NewExperience(ExperienceInput{ExperienceID: "exp-1", ActionID: "a-1", RouteID: "r-1", ReceiptID: "rcpt-1", ReceiptResultHash: strings.Repeat("a", 64), Context: ctx, SelectedAction: "workers=4", Reward: 12, RewardUnit: "MiB/s", PolicyStatsBefore: before, PolicyStatsAfter: after})
	if err != nil {
		t.Fatal(err)
	}
	if e.Ref() != "adaptive-experience://exp-1" {
		t.Fatalf("unexpected ref %q", e.Ref())
	}
	after["workers=4"] = ActionStat{Count: 2, MeanReward: 11}
	if _, err := NewExperience(ExperienceInput{ExperienceID: "exp-2", ActionID: "a-2", RouteID: "r-2", ReceiptID: "rcpt-2", ReceiptResultHash: strings.Repeat("b", 64), Context: ctx, SelectedAction: "workers=4", Reward: 12, RewardUnit: "MiB/s", PolicyStatsBefore: before, PolicyStatsAfter: after}); err == nil {
		t.Fatal("expected non-advancing evidence to fail")
	}
}

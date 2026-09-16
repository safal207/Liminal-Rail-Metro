package adaptive

import "testing"

func TestPolicyExploresEveryAllowedActionBeforeRepeating(t *testing.T) {
	p, err := NewUCB1Policy([]string{"workers=1", "workers=2", "workers=3"}, 1.0)
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		d := p.Choose()
		if seen[d.Action] {
			t.Fatalf("action %q repeated before all actions were explored", d.Action)
		}
		seen[d.Action] = true
		if err := p.Observe(d.Action, float64(i+1)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPolicyLearnsHigherRewardAction(t *testing.T) {
	p, err := NewUCB1Policy([]string{"slow", "fast"}, 0.4)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 100; i++ {
		d := p.Choose()
		reward := 1.0
		if d.Action == "fast" {
			reward = 10.0
		}
		if err := p.Observe(d.Action, reward); err != nil {
			t.Fatal(err)
		}
	}

	best, stat, ok := p.BestObserved()
	if !ok {
		t.Fatal("expected a best action")
	}
	if best != "fast" {
		t.Fatalf("best action = %q, want fast", best)
	}
	if stat.MeanReward != 10 {
		t.Fatalf("fast mean reward = %v, want 10", stat.MeanReward)
	}
}

func TestPolicyRejectsOutOfAllowListObservation(t *testing.T) {
	p, err := NewUCB1Policy([]string{"safe"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Observe("arbitrary-shell-command", 1000); err == nil {
		t.Fatal("expected out-of-allow-list observation to fail")
	}
}

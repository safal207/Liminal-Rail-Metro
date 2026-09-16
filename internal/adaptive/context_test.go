package adaptive

import "testing"

func TestContextualPolicyLearnsDifferentActionsPerContext(t *testing.T) {
	p, err := NewContextualUCB1Policy([]string{"workers=1", "workers=4"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	cpu := Context{Workload: "sha256", CPUClass: "3-4", LoadClass: "low", MemoryClass: "high"}
	latency := Context{Workload: "latency", CPUClass: "3-4", LoadClass: "low", MemoryClass: "high"}
	for i := 0; i < 12; i++ {
		d, _ := p.Choose(cpu)
		r := 1.0
		if d.Decision.Action == "workers=4" {
			r = 9
		}
		if err := p.Observe(cpu, d.Decision.Action, r); err != nil {
			t.Fatal(err)
		}
		d, _ = p.Choose(latency)
		r = 9
		if d.Decision.Action == "workers=4" {
			r = 1
		}
		if err := p.Observe(latency, d.Decision.Action, r); err != nil {
			t.Fatal(err)
		}
	}
	bestCPU, _, _, _ := p.BestObserved(cpu)
	bestLatency, _, _, _ := p.BestObserved(latency)
	if bestCPU != "workers=4" || bestLatency != "workers=1" {
		t.Fatalf("best cpu=%q latency=%q", bestCPU, bestLatency)
	}
}

func TestContextKeyChangesWithWorkload(t *testing.T) {
	state := HostState{LogicalCPUs: 8, Load1: 1, MemAvailableMiB: 4096}
	a, _ := ContextFromHost("sha256", state)
	b, _ := ContextFromHost("json", state)
	ka, _ := a.Key()
	kb, _ := b.Key()
	if ka == kb {
		t.Fatal("different workloads must not share a context key")
	}
}

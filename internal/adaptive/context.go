package adaptive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
)

// Context is a deliberately coarse runtime state. Bucketing prevents the
// policy from creating a new learning table for every tiny telemetry change.
type Context struct {
	Workload    string `json:"workload"`
	CPUClass    string `json:"cpu_class"`
	LoadClass   string `json:"load_class"`
	MemoryClass string `json:"memory_class"`
}

func ContextFromHost(workload string, state HostState) (Context, error) {
	if workload == "" {
		return Context{}, errors.New("workload is required")
	}
	return Context{Workload: workload, CPUClass: cpuClass(state.LogicalCPUs), LoadClass: loadClass(state), MemoryClass: memoryClass(state.MemAvailableMiB)}, nil
}

func (c Context) Key() (string, error) {
	if c.Workload == "" || c.CPUClass == "" || c.LoadClass == "" || c.MemoryClass == "" {
		return "", errors.New("context fields must be non-empty")
	}
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return "ctx-" + hex.EncodeToString(s[:8]), nil
}

func cpuClass(n int) string {
	switch {
	case n <= 1:
		return "1"
	case n <= 2:
		return "2"
	case n <= 4:
		return "3-4"
	case n <= 8:
		return "5-8"
	case n <= 16:
		return "9-16"
	default:
		return "17+"
	}
}
func loadClass(state HostState) string {
	cpus := state.LogicalCPUs
	if cpus < 1 {
		cpus = 1
	}
	ratio := state.Load1 / float64(cpus)
	switch {
	case ratio < 0.35:
		return "low"
	case ratio < 0.8:
		return "medium"
	default:
		return "high"
	}
}
func memoryClass(mib float64) string {
	switch {
	case mib <= 0:
		return "unknown"
	case mib < 512:
		return "constrained"
	case mib < 2048:
		return "low"
	case mib < 8192:
		return "medium"
	default:
		return "high"
	}
}

type ContextDecision struct {
	ContextKey string   `json:"context_key"`
	Decision   Decision `json:"decision"`
}

// ContextualUCB1Policy keeps independent learned evidence per coarse context.
// It still cannot select anything outside the construction-time allow-list.
type ContextualUCB1Policy struct {
	mu          sync.Mutex
	actions     []string
	exploration float64
	policies    map[string]*UCB1Policy
}

func NewContextualUCB1Policy(actions []string, exploration float64) (*ContextualUCB1Policy, error) {
	if _, err := NewUCB1Policy(actions, exploration); err != nil {
		return nil, err
	}
	return &ContextualUCB1Policy{actions: append([]string(nil), actions...), exploration: exploration, policies: map[string]*UCB1Policy{}}, nil
}

func (p *ContextualUCB1Policy) Choose(ctx Context) (ContextDecision, error) {
	policy, key, err := p.policyFor(ctx)
	if err != nil {
		return ContextDecision{}, err
	}
	return ContextDecision{ContextKey: key, Decision: policy.Choose()}, nil
}
func (p *ContextualUCB1Policy) Observe(ctx Context, action string, reward float64) error {
	policy, _, err := p.policyFor(ctx)
	if err != nil {
		return err
	}
	return policy.Observe(action, reward)
}
func (p *ContextualUCB1Policy) SnapshotContext(ctx Context) (map[string]ActionStat, error) {
	policy, _, err := p.policyFor(ctx)
	if err != nil {
		return nil, err
	}
	return policy.Snapshot(), nil
}
func (p *ContextualUCB1Policy) BestObserved(ctx Context) (string, ActionStat, bool, error) {
	policy, _, err := p.policyFor(ctx)
	if err != nil {
		return "", ActionStat{}, false, err
	}
	a, s, ok := policy.BestObserved()
	return a, s, ok, nil
}
func (p *ContextualUCB1Policy) policyFor(ctx Context) (*UCB1Policy, string, error) {
	key, err := ctx.Key()
	if err != nil {
		return nil, "", err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if existing, ok := p.policies[key]; ok {
		return existing, key, nil
	}
	created, err := NewUCB1Policy(p.actions, p.exploration)
	if err != nil {
		return nil, "", err
	}
	p.policies[key] = created
	return created, key, nil
}

package adaptive

import (
	"errors"
	"math"
	"sort"
	"sync"
)

// ActionStat is the learned evidence for one bounded action.
type ActionStat struct {
	Count      int     `json:"count"`
	MeanReward float64 `json:"mean_reward"`
}

// Decision records why an action was selected at one policy step.
type Decision struct {
	Action string                `json:"action"`
	Scores map[string]float64    `json:"scores"`
	Stats  map[string]ActionStat `json:"stats"`
}

// UCB1Policy learns which action performs best from measured rewards.
// It can only select from the allow-list supplied at construction time.
type UCB1Policy struct {
	mu          sync.Mutex
	actions     []string
	stats       map[string]ActionStat
	total       int
	exploration float64
}

// NewUCB1Policy constructs an allow-listed online policy.
// exploration controls how strongly the policy samples less-tested actions.
func NewUCB1Policy(actions []string, exploration float64) (*UCB1Policy, error) {
	if len(actions) == 0 {
		return nil, errors.New("adaptive policy requires at least one action")
	}
	if exploration < 0 {
		return nil, errors.New("exploration must be >= 0")
	}

	seen := make(map[string]struct{}, len(actions))
	ordered := make([]string, 0, len(actions))
	stats := make(map[string]ActionStat, len(actions))
	for _, action := range actions {
		if action == "" {
			return nil, errors.New("adaptive action must not be empty")
		}
		if _, ok := seen[action]; ok {
			return nil, errors.New("adaptive actions must be unique")
		}
		seen[action] = struct{}{}
		ordered = append(ordered, action)
		stats[action] = ActionStat{}
	}

	return &UCB1Policy{
		actions:     ordered,
		stats:       stats,
		exploration: exploration,
	}, nil
}

// Choose returns one allowed action. Each action is sampled once before UCB1
// exploitation/exploration begins.
func (p *UCB1Policy) Choose() Decision {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, action := range p.actions {
		if p.stats[action].Count == 0 {
			return Decision{
				Action: action,
				Scores: map[string]float64{action: math.Inf(1)},
				Stats:  cloneStats(p.stats),
			}
		}
	}

	scores := make(map[string]float64, len(p.actions))
	bestAction := p.actions[0]
	bestScore := math.Inf(-1)
	for _, action := range p.actions {
		stat := p.stats[action]
		bonus := p.exploration * math.Sqrt(math.Log(float64(p.total))/float64(stat.Count))
		score := stat.MeanReward + bonus
		scores[action] = score
		if score > bestScore {
			bestScore = score
			bestAction = action
		}
	}

	return Decision{Action: bestAction, Scores: scores, Stats: cloneStats(p.stats)}
}

// Observe updates the running reward estimate for an allowed action.
func (p *UCB1Policy) Observe(action string, reward float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	stat, ok := p.stats[action]
	if !ok {
		return errors.New("cannot observe reward for action outside allow-list")
	}
	if math.IsNaN(reward) || math.IsInf(reward, 0) {
		return errors.New("reward must be finite")
	}

	stat.Count++
	stat.MeanReward += (reward - stat.MeanReward) / float64(stat.Count)
	p.stats[action] = stat
	p.total++
	return nil
}

// Snapshot returns a copy of learned statistics.
func (p *UCB1Policy) Snapshot() map[string]ActionStat {
	p.mu.Lock()
	defer p.mu.Unlock()
	return cloneStats(p.stats)
}

// BestObserved returns the action with the highest measured mean reward.
// Ties are deterministic and follow lexical order.
func (p *UCB1Policy) BestObserved() (string, ActionStat, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	keys := append([]string(nil), p.actions...)
	sort.Strings(keys)
	best := ""
	var bestStat ActionStat
	found := false
	for _, action := range keys {
		stat := p.stats[action]
		if stat.Count == 0 {
			continue
		}
		if !found || stat.MeanReward > bestStat.MeanReward {
			best = action
			bestStat = stat
			found = true
		}
	}
	return best, bestStat, found
}

func cloneStats(src map[string]ActionStat) map[string]ActionStat {
	dst := make(map[string]ActionStat, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

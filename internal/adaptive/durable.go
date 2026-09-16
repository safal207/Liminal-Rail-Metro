package adaptive

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const DurableEntryProtocol = "liminal.adaptive.reward-entry.v0.1"

type DurableEntry struct {
	Protocol          string         `json:"protocol"`
	Sequence          uint64         `json:"sequence"`
	Receipt           metro.Receipt  `json:"receipt"`
	ReceiptHash       string         `json:"receipt_hash"`
	Result            map[string]any `json:"result"`
	Experience        Experience     `json:"experience"`
	ExperienceHash    string         `json:"experience_hash"`
	PreviousEntryHash string         `json:"previous_entry_hash,omitempty"`
	EntryHash         string         `json:"entry_hash"`
	AppliedAt         string         `json:"applied_at"`
}

type ApplyResult struct {
	Applied        bool   `json:"applied"`
	Duplicate      bool   `json:"duplicate"`
	Sequence       uint64 `json:"sequence"`
	EntryHash      string `json:"entry_hash,omitempty"`
	ReceiptHash    string `json:"receipt_hash,omitempty"`
	ExperienceHash string `json:"experience_hash,omitempty"`
}

type DurableLearner struct {
	mu               sync.Mutex
	path             string
	policy           *ContextualUCB1Policy
	receiptHashes    map[string]string
	experienceHashes map[string]string
	actionReceipts   map[string]string
	lastEntryHash    string
	sequence         uint64
}

func OpenDurableLearner(path string, actions []string, exploration float64) (*DurableLearner, error) {
	if path == "" {
		return nil, errors.New("journal path is required")
	}
	policy, err := NewContextualUCB1Policy(actions, exploration)
	if err != nil {
		return nil, err
	}
	d := &DurableLearner{
		path:             path,
		policy:           policy,
		receiptHashes:    map[string]string{},
		experienceHashes: map[string]string{},
		actionReceipts:   map[string]string{},
	}
	if err := d.replay(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *DurableLearner) Choose(ctx Context) (ContextDecision, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.policy.Choose(ctx)
}

func (d *DurableLearner) SnapshotContext(ctx Context) (map[string]ActionStat, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.policy.SnapshotContext(ctx)
}

func (d *DurableLearner) BestObserved(ctx Context) (string, ActionStat, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.policy.BestObserved(ctx)
}

func (d *DurableLearner) JournalHead() (uint64, string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sequence, d.lastEntryHash
}

func PredictStatsAfter(before map[string]ActionStat, action string, reward float64) (map[string]ActionStat, error) {
	if math.IsNaN(reward) || math.IsInf(reward, 0) {
		return nil, errors.New("reward must be finite")
	}
	after := cloneStats(before)
	stat, ok := after[action]
	if !ok {
		return nil, errors.New("selected action missing from policy stats")
	}
	stat.Count++
	stat.MeanReward += (reward - stat.MeanReward) / float64(stat.Count)
	after[action] = stat
	return after, nil
}

func (d *DurableLearner) Apply(receipt metro.Receipt, result map[string]any, exp Experience) (ApplyResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	receiptHash, experienceHash, err := validateMeasurement(receipt, result, exp)
	if err != nil {
		return ApplyResult{}, err
	}

	if existing, ok := d.receiptHashes[receipt.ReceiptID]; ok {
		if existing == receiptHash && d.experienceHashes[exp.ExperienceID] == experienceHash && d.actionReceipts[exp.ActionID] == receipt.ReceiptID {
			return ApplyResult{Duplicate: true, Sequence: d.sequence, EntryHash: d.lastEntryHash, ReceiptHash: receiptHash, ExperienceHash: experienceHash}, nil
		}
		return ApplyResult{}, errors.New("receipt identity conflicts with previously applied evidence")
	}
	if _, ok := d.experienceHashes[exp.ExperienceID]; ok {
		return ApplyResult{}, errors.New("experience identity already applied with different evidence")
	}
	if priorReceipt, ok := d.actionReceipts[exp.ActionID]; ok {
		return ApplyResult{}, fmt.Errorf("action %q already learned from receipt %q", exp.ActionID, priorReceipt)
	}

	current, err := d.policy.SnapshotContext(exp.Context)
	if err != nil {
		return ApplyResult{}, err
	}
	if !sameStats(current, exp.PolicyStatsBefore) {
		return ApplyResult{}, errors.New("experience pre-policy state does not match durable learner state")
	}
	predicted, err := PredictStatsAfter(current, exp.SelectedAction, exp.Reward)
	if err != nil {
		return ApplyResult{}, err
	}
	if !sameStats(predicted, exp.PolicyStatsAfter) {
		return ApplyResult{}, errors.New("experience post-policy state does not match measured reward transition")
	}

	entry := DurableEntry{
		Protocol:          DurableEntryProtocol,
		Sequence:          d.sequence + 1,
		Receipt:           receipt,
		ReceiptHash:       receiptHash,
		Result:            cloneResult(result),
		Experience:        exp,
		ExperienceHash:    experienceHash,
		PreviousEntryHash: d.lastEntryHash,
		AppliedAt:         time.Now().UTC().Format(time.RFC3339Nano),
	}
	entry.EntryHash, err = hashEntry(entry)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := d.appendEntry(entry); err != nil {
		return ApplyResult{}, err
	}

	if err := d.policy.Observe(exp.Context, exp.SelectedAction, exp.Reward); err != nil {
		return ApplyResult{}, fmt.Errorf("journal committed but in-memory observation failed: %w", err)
	}
	after, err := d.policy.SnapshotContext(exp.Context)
	if err != nil {
		return ApplyResult{}, err
	}
	if !sameStats(after, exp.PolicyStatsAfter) {
		return ApplyResult{}, errors.New("journal committed but in-memory policy transition diverged")
	}

	d.recordIdentity(entry)
	d.sequence = entry.Sequence
	d.lastEntryHash = entry.EntryHash
	return ApplyResult{Applied: true, Sequence: entry.Sequence, EntryHash: entry.EntryHash, ReceiptHash: receiptHash, ExperienceHash: experienceHash}, nil
}

func (d *DurableLearner) replay() error {
	f, err := os.Open(d.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var entry DurableEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return fmt.Errorf("journal line %d is not valid JSON: %w", line, err)
		}
		if entry.Protocol != DurableEntryProtocol {
			return fmt.Errorf("journal line %d has unsupported protocol %q", line, entry.Protocol)
		}
		if entry.Sequence != d.sequence+1 {
			return fmt.Errorf("journal line %d sequence mismatch", line)
		}
		if entry.PreviousEntryHash != d.lastEntryHash {
			return fmt.Errorf("journal line %d hash-chain predecessor mismatch", line)
		}
		expectedEntryHash, err := hashEntry(entry)
		if err != nil {
			return fmt.Errorf("journal line %d hash: %w", line, err)
		}
		if entry.EntryHash != expectedEntryHash {
			return fmt.Errorf("journal line %d entry hash mismatch", line)
		}
		receiptHash, experienceHash, err := validateMeasurement(entry.Receipt, entry.Result, entry.Experience)
		if err != nil {
			return fmt.Errorf("journal line %d evidence invalid: %w", line, err)
		}
		if receiptHash != entry.ReceiptHash || experienceHash != entry.ExperienceHash {
			return fmt.Errorf("journal line %d content-address mismatch", line)
		}
		if _, ok := d.receiptHashes[entry.Receipt.ReceiptID]; ok {
			return fmt.Errorf("journal line %d duplicates a receipt identity", line)
		}
		if _, ok := d.experienceHashes[entry.Experience.ExperienceID]; ok {
			return fmt.Errorf("journal line %d duplicates an experience identity", line)
		}
		if _, ok := d.actionReceipts[entry.Experience.ActionID]; ok {
			return fmt.Errorf("journal line %d duplicates an action identity", line)
		}
		current, err := d.policy.SnapshotContext(entry.Experience.Context)
		if err != nil {
			return err
		}
		if !sameStats(current, entry.Experience.PolicyStatsBefore) {
			return fmt.Errorf("journal line %d pre-policy state mismatch", line)
		}
		predicted, err := PredictStatsAfter(current, entry.Experience.SelectedAction, entry.Experience.Reward)
		if err != nil {
			return err
		}
		if !sameStats(predicted, entry.Experience.PolicyStatsAfter) {
			return fmt.Errorf("journal line %d policy transition mismatch", line)
		}
		if err := d.policy.Observe(entry.Experience.Context, entry.Experience.SelectedAction, entry.Experience.Reward); err != nil {
			return err
		}
		d.recordIdentity(entry)
		d.sequence = entry.Sequence
		d.lastEntryHash = entry.EntryHash
	}
	return scanner.Err()
}

func validateMeasurement(receipt metro.Receipt, result map[string]any, exp Experience) (string, string, error) {
	if receipt.Protocol != metro.ReceiptProtocol {
		return "", "", errors.New("unsupported receipt protocol")
	}
	if receipt.Status != "SUCCEEDED" {
		return "", "", fmt.Errorf("receipt status %q is not verified learning evidence", receipt.Status)
	}
	if receipt.HashAlgorithm != "sha256" || !isSHA256Hex(receipt.ResultHash) {
		return "", "", errors.New("receipt requires a sha256 result hash")
	}
	if exp.Protocol != ExperienceProtocol {
		return "", "", errors.New("unsupported experience protocol")
	}
	key, err := exp.Context.Key()
	if err != nil {
		return "", "", err
	}
	if exp.ContextKey != key {
		return "", "", errors.New("experience context key mismatch")
	}
	if receipt.ReceiptID != exp.ReceiptID || receipt.ActionID != exp.ActionID || receipt.RouteID != exp.RouteID {
		return "", "", errors.New("receipt identity does not match experience provenance")
	}
	if receipt.ExecutorID != exp.SelectedAction {
		return "", "", errors.New("receipt executor does not match selected adaptive action")
	}
	if receipt.ResultRef != exp.Ref() {
		return "", "", errors.New("receipt result_ref does not identify the adaptive experience")
	}
	if receipt.ResultHash != exp.ReceiptResultHash {
		return "", "", errors.New("experience result hash does not match receipt")
	}
	resultHash, err := metro.HashJSON(result)
	if err != nil {
		return "", "", err
	}
	if resultHash != receipt.ResultHash {
		return "", "", errors.New("measured result payload does not match receipt result hash")
	}
	if valueString(result, "selected_action") != exp.SelectedAction || valueString(result, "context_key") != exp.ContextKey || valueString(result, "reward_unit") != exp.RewardUnit {
		return "", "", errors.New("measured result metadata does not match experience")
	}
	reward, ok := valueFloat(result, "reward")
	if !ok || !almostEqual(reward, exp.Reward) {
		return "", "", errors.New("measured result reward does not match experience")
	}
	if math.IsNaN(exp.Reward) || math.IsInf(exp.Reward, 0) {
		return "", "", errors.New("experience reward must be finite")
	}
	receiptHash, err := metro.HashJSON(receipt)
	if err != nil {
		return "", "", err
	}
	experienceHash, err := metro.HashJSON(exp)
	if err != nil {
		return "", "", err
	}
	return receiptHash, experienceHash, nil
}

func hashEntry(entry DurableEntry) (string, error) {
	copy := entry
	copy.EntryHash = ""
	return metro.HashJSON(copy)
}

func (d *DurableLearner) appendEntry(entry DurableEntry) error {
	if err := os.MkdirAll(filepath.Dir(d.path), 0o755); err != nil && filepath.Dir(d.path) != "." {
		return err
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(d.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

func (d *DurableLearner) recordIdentity(entry DurableEntry) {
	d.receiptHashes[entry.Receipt.ReceiptID] = entry.ReceiptHash
	d.experienceHashes[entry.Experience.ExperienceID] = entry.ExperienceHash
	d.actionReceipts[entry.Experience.ActionID] = entry.Receipt.ReceiptID
}

func sameStats(a, b map[string]ActionStat) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || av.Count != bv.Count || !almostEqual(av.MeanReward, bv.MeanReward) {
			return false
		}
	}
	return true
}

func almostEqual(a, b float64) bool {
	scale := math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
	return math.Abs(a-b) <= 1e-9*scale
}

func valueString(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func valueFloat(m map[string]any, key string) (float64, bool) {
	switch v := m[key].(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}

func cloneResult(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

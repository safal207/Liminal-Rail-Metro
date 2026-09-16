package adaptive

import (
	"encoding/hex"
	"errors"
	"math"
	"strings"
	"time"
)

const ExperienceProtocol = "liminal.adaptive.experience.v0.1"

type Experience struct {
	Protocol              string                `json:"protocol"`
	ExperienceID          string                `json:"experience_id"`
	ActionID              string                `json:"action_id"`
	RouteID               string                `json:"route_id"`
	ReceiptID             string                `json:"receipt_id"`
	ReceiptResultHash     string                `json:"receipt_result_hash"`
	Context               Context               `json:"context"`
	ContextKey            string                `json:"context_key"`
	SelectedAction        string                `json:"selected_action"`
	Reward                float64               `json:"reward"`
	RewardUnit            string                `json:"reward_unit"`
	PolicyStatsBefore     map[string]ActionStat `json:"policy_stats_before"`
	PolicyStatsAfter      map[string]ActionStat `json:"policy_stats_after"`
	PreviousExperienceRef string                `json:"previous_experience_ref,omitempty"`
	MeasuredAt            string                `json:"measured_at"`
}

type ExperienceInput struct {
	ExperienceID, ActionID, RouteID, ReceiptID, ReceiptResultHash string
	Context                                                       Context
	SelectedAction                                                string
	Reward                                                        float64
	RewardUnit                                                    string
	PolicyStatsBefore, PolicyStatsAfter                           map[string]ActionStat
	PreviousExperienceRef                                         string
}

func NewExperience(in ExperienceInput) (Experience, error) {
	key, err := in.Context.Key()
	if err != nil {
		return Experience{}, err
	}
	if in.ExperienceID == "" || in.ActionID == "" || in.RouteID == "" || in.ReceiptID == "" || in.ReceiptResultHash == "" {
		return Experience{}, errors.New("experience requires action, route, receipt, result hash, and experience identity")
	}
	if !isSHA256Hex(in.ReceiptResultHash) {
		return Experience{}, errors.New("receipt result hash must be a lowercase sha256 hex digest")
	}
	if in.SelectedAction == "" || in.RewardUnit == "" {
		return Experience{}, errors.New("selected action and reward unit are required")
	}
	if math.IsNaN(in.Reward) || math.IsInf(in.Reward, 0) {
		return Experience{}, errors.New("reward must be finite")
	}
	if _, ok := in.PolicyStatsBefore[in.SelectedAction]; !ok {
		return Experience{}, errors.New("selected action missing from pre-decision policy stats")
	}
	if after, ok := in.PolicyStatsAfter[in.SelectedAction]; !ok || after.Count <= in.PolicyStatsBefore[in.SelectedAction].Count {
		return Experience{}, errors.New("post-observation stats must advance selected action evidence")
	}
	return Experience{Protocol: ExperienceProtocol, ExperienceID: in.ExperienceID, ActionID: in.ActionID, RouteID: in.RouteID, ReceiptID: in.ReceiptID, ReceiptResultHash: in.ReceiptResultHash, Context: in.Context, ContextKey: key, SelectedAction: in.SelectedAction, Reward: in.Reward, RewardUnit: in.RewardUnit, PolicyStatsBefore: cloneStats(in.PolicyStatsBefore), PolicyStatsAfter: cloneStats(in.PolicyStatsAfter), PreviousExperienceRef: in.PreviousExperienceRef, MeasuredAt: time.Now().UTC().Format(time.RFC3339Nano)}, nil
}

func (e Experience) Ref() string { return "adaptive-experience://" + e.ExperienceID }

func isSHA256Hex(v string) bool {
	if len(v) != 64 {
		return false
	}
	b, err := hex.DecodeString(v)
	return err == nil && len(b) == 32 && v == strings.ToLower(v)
}

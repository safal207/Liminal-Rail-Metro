package metro

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	PacketProtocol  = "metro.packet.v0.1"
	RouteProtocol   = "metro.route.v0.1"
	ReceiptProtocol = "metro.receipt.v0.1"
)

type Action struct {
	Kind   string         `json:"kind"`
	Inputs map[string]any `json:"inputs"`
}

type Constraints struct {
	TimeoutMS  int  `json:"timeout_ms,omitempty"`
	SideEffect bool `json:"side_effect"`
}

type Packet struct {
	Protocol           string      `json:"protocol"`
	ActionID           string      `json:"action_id"`
	SourceAgent        string      `json:"source_agent"`
	CreatedAt          string      `json:"created_at"`
	Goal               string      `json:"goal"`
	Action             Action      `json:"action"`
	AllowedTargets     []string    `json:"allowed_targets"`
	ContextRefs        []string    `json:"context_refs,omitempty"`
	Constraints        Constraints `json:"constraints,omitempty"`
	PreviousReceiptRef string      `json:"previous_receipt_ref,omitempty"`
}

type Candidate struct {
	Target string  `json:"target"`
	Score  float64 `json:"score,omitempty"`
}

type Route struct {
	Protocol       string      `json:"protocol"`
	RouteID        string      `json:"route_id"`
	ActionID       string      `json:"action_id"`
	RouterID       string      `json:"router_id"`
	DecisionMode   string      `json:"decision_mode"`
	SelectedTarget string      `json:"selected_target"`
	Candidates     []Candidate `json:"candidates,omitempty"`
	Confidence     float64     `json:"confidence,omitempty"`
	PolicyRef      string      `json:"policy_ref,omitempty"`
	DecidedAt      string      `json:"decided_at"`
}

type Receipt struct {
	Protocol      string `json:"protocol"`
	ReceiptID     string `json:"receipt_id"`
	ActionID      string `json:"action_id"`
	RouteID       string `json:"route_id"`
	ExecutorID    string `json:"executor_id"`
	Status        string `json:"status"`
	HashAlgorithm string `json:"hash_algorithm"`
	InputHash     string `json:"input_hash"`
	ResultHash    string `json:"result_hash,omitempty"`
	ResultRef     string `json:"result_ref,omitempty"`
	StartedAt     string `json:"started_at"`
	CompletedAt   string `json:"completed_at"`
}

type Router struct {
	ID     string
	Policy map[string]string
}

func NewPacket(actionID, sourceAgent, goal string, action Action, allowedTargets []string) Packet {
	return Packet{
		Protocol:       PacketProtocol,
		ActionID:       actionID,
		SourceAgent:    sourceAgent,
		CreatedAt:      NowISO(),
		Goal:           goal,
		Action:         action,
		AllowedTargets: allowedTargets,
	}
}

func (r Router) Route(packet Packet) (Route, error) {
	target, ok := r.Policy[packet.Action.Kind]
	if !ok {
		return Route{}, fmt.Errorf("no route for action kind %q", packet.Action.Kind)
	}
	if !Contains(packet.AllowedTargets, target) {
		return Route{}, fmt.Errorf("router selected disallowed target %q for action %s", target, packet.ActionID)
	}

	return Route{
		Protocol:       RouteProtocol,
		RouteID:        "route-" + packet.ActionID,
		ActionID:       packet.ActionID,
		RouterID:       r.ID,
		DecisionMode:   "deterministic",
		SelectedTarget: target,
		Candidates:     []Candidate{{Target: target, Score: 1}},
		Confidence:     1,
		PolicyRef:      "policy://demo/route-by-action-kind",
		DecidedAt:      NowISO(),
	}, nil
}

func MakeSuccessReceipt(packet Packet, route Route, result map[string]any, resultRef string) (Receipt, error) {
	if route.ActionID != packet.ActionID {
		return Receipt{}, errors.New("route is not bound to packet action_id")
	}
	if !Contains(packet.AllowedTargets, route.SelectedTarget) {
		return Receipt{}, errors.New("route target is not allowed by packet")
	}

	inputHash, err := HashJSON(packet.Action.Inputs)
	if err != nil {
		return Receipt{}, err
	}
	resultHash, err := HashJSON(result)
	if err != nil {
		return Receipt{}, err
	}

	started := NowISO()
	return Receipt{
		Protocol:      ReceiptProtocol,
		ReceiptID:     "receipt-" + packet.ActionID,
		ActionID:      packet.ActionID,
		RouteID:       route.RouteID,
		ExecutorID:    route.SelectedTarget,
		Status:        "SUCCEEDED",
		HashAlgorithm: "sha256",
		InputHash:     inputHash,
		ResultHash:    resultHash,
		ResultRef:     resultRef,
		StartedAt:     started,
		CompletedAt:   NowISO(),
	}, nil
}

func Verify(packet Packet, route Route, result map[string]any, receipt Receipt) error {
	if receipt.ActionID != packet.ActionID {
		return errors.New("receipt action_id mismatch")
	}
	if receipt.RouteID != route.RouteID {
		return errors.New("receipt route_id mismatch")
	}
	if receipt.ExecutorID != route.SelectedTarget {
		return errors.New("receipt executor mismatch")
	}
	if !Contains(packet.AllowedTargets, route.SelectedTarget) {
		return errors.New("selected target not allowed")
	}

	inputHash, err := HashJSON(packet.Action.Inputs)
	if err != nil {
		return err
	}
	if receipt.InputHash != inputHash {
		return errors.New("input hash mismatch")
	}

	resultHash, err := HashJSON(result)
	if err != nil {
		return err
	}
	if receipt.ResultHash != resultHash {
		return errors.New("result hash mismatch")
	}
	return nil
}

func HashJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func Contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func NowISO() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

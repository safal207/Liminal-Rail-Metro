package codexplugin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/safal207/Liminal-Rail-Metro/internal/decisionplane"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const Version = "0.1.0"

type Config struct {
	RemoteEndpoint         string
	RemoteProviderID       string
	RemoteBearerToken      string
	RemoteTimeout          time.Duration
	RemoteMaxResponseBytes int64
}

type Runtime struct {
	provider     decisionplane.Provider
	providerMode string
	providerID   string
}

type ChoiceInput struct {
	ID     string `json:"id" jsonschema:"stable choice identifier"`
	Target string `json:"target" jsonschema:"bounded Metro target"`
	Label  string `json:"label,omitempty" jsonschema:"human-readable choice label"`
}

type DecideInput struct {
	RequestID   string             `json:"request_id" jsonschema:"stable decision request identifier"`
	ActionID    string             `json:"action_id" jsonschema:"stable logical action identifier"`
	SourceAgent string             `json:"source_agent,omitempty" jsonschema:"agent originating the action; defaults to codex"`
	Goal        string             `json:"goal" jsonschema:"bounded goal for this action"`
	Kind        string             `json:"kind" jsonschema:"bounded action kind"`
	Inputs      map[string]any     `json:"inputs,omitempty" jsonschema:"bounded action inputs"`
	State       map[string]any     `json:"state,omitempty" jsonschema:"decision state to bind by hash"`
	Choices     []ChoiceInput      `json:"choices" jsonschema:"allowed semantic choices; targets must be unique"`
	Scores      map[string]float64 `json:"scores,omitempty" jsonschema:"local proof-mode non-negative scores keyed by choice id; ignored when a remote provider is configured"`
	SideEffect  bool               `json:"side_effect" jsonschema:"whether the action can create an external side effect"`
	TimeoutMS   int                `json:"timeout_ms,omitempty" jsonschema:"optional execution timeout carried into the Metro packet"`
}

type DecideOutput struct {
	RequestID      string       `json:"request_id"`
	ActionID       string       `json:"action_id"`
	ProviderID     string       `json:"provider_id"`
	ProviderMode   string       `json:"provider_mode"`
	Disposition    string       `json:"disposition"`
	ReasonCode     string       `json:"reason_code"`
	SelectedTarget string       `json:"selected_target,omitempty"`
	Confidence     float64      `json:"confidence"`
	TopMargin      float64      `json:"top_margin"`
	PacketHash     string       `json:"packet_hash"`
	StateHash      string       `json:"state_hash"`
	ChoicesHash    string       `json:"choices_hash"`
	Route          *metro.Route `json:"route,omitempty"`
}

type VerifyInput struct {
	Packet  metro.Packet    `json:"packet" jsonschema:"original Metro packet"`
	Route   metro.Route     `json:"route" jsonschema:"route used for execution"`
	Result  map[string]any  `json:"result" jsonschema:"claimed execution result"`
	Receipt metro.Receipt   `json:"receipt" jsonschema:"execution receipt to verify"`
}

type VerifyOutput struct {
	Verified   bool   `json:"verified"`
	ActionID   string `json:"action_id"`
	RouteID    string `json:"route_id"`
	ExecutorID string `json:"executor_id"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
}

type StatusInput struct{}

type StatusOutput struct {
	Version              string   `json:"version"`
	ProviderMode         string   `json:"provider_mode"`
	ProviderID           string   `json:"provider_id"`
	Tools                []string `json:"tools"`
	DecisionBinding      string   `json:"decision_binding"`
	SideEffectPolicy     string   `json:"side_effect_policy"`
	CompletionPolicy     string   `json:"completion_policy"`
	Transport            string   `json:"transport"`
	PublicDeploymentNote string   `json:"public_deployment_note"`
}

func NewRuntime(config Config) (*Runtime, error) {
	if config.RemoteEndpoint == "" {
		return &Runtime{providerMode: "static-proof", providerID: "codex-static-proof"}, nil
	}
	if config.RemoteProviderID == "" {
		return nil, errors.New("remote provider id is required when remote endpoint is configured")
	}
	timeout := config.RemoteTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	provider, err := decisionplane.NewRemoteProvider(decisionplane.RemoteProviderConfig{
		Endpoint:         config.RemoteEndpoint,
		ProviderID:       config.RemoteProviderID,
		BearerToken:      config.RemoteBearerToken,
		Timeout:          timeout,
		MaxResponseBytes: config.RemoteMaxResponseBytes,
	})
	if err != nil {
		return nil, err
	}
	return &Runtime{
		provider:     provider,
		providerMode: "remote",
		providerID:   config.RemoteProviderID,
	}, nil
}

func NewRuntimeFromEnv() (*Runtime, error) {
	timeout, err := durationFromEnvMillis("LIMINAL_REMOTE_TIMEOUT_MS")
	if err != nil {
		return nil, err
	}
	maxBytes, err := int64FromEnv("LIMINAL_REMOTE_MAX_RESPONSE_BYTES")
	if err != nil {
		return nil, err
	}
	return NewRuntime(Config{
		RemoteEndpoint:         strings.TrimSpace(os.Getenv("LIMINAL_REMOTE_PROVIDER_URL")),
		RemoteProviderID:       strings.TrimSpace(os.Getenv("LIMINAL_REMOTE_PROVIDER_ID")),
		RemoteBearerToken:      os.Getenv("LIMINAL_REMOTE_PROVIDER_TOKEN"),
		RemoteTimeout:          timeout,
		RemoteMaxResponseBytes: maxBytes,
	})
}

func (runtime *Runtime) NewMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "liminal-rail-codex",
		Version: Version,
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "liminal_decide",
		Description: "Create a bounded Metro decision. Returns AUTO_ROUTE only when the decision is bound to the packet/state/choices and satisfies the confidence, margin, and side-effect policy gates.",
	}, runtime.decide)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "liminal_verify",
		Description: "Verify an execution receipt against the original Metro packet, route, and claimed result. UNKNOWN or non-SUCCEEDED receipts are never accepted as completion proof.",
	}, runtime.verify)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "liminal_status",
		Description: "Report Liminal Rail Codex plugin runtime mode and proof-boundary capabilities. This is read-only and does not claim live station metrics.",
	}, runtime.status)

	return server
}

func MCPHandler(server *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			Stateless:    true,
			JSONResponse: true,
		},
	)
}

func (runtime *Runtime) decide(ctx context.Context, _ *mcp.CallToolRequest, input DecideInput) (*mcp.CallToolResult, DecideOutput, error) {
	if input.RequestID == "" || input.ActionID == "" || input.Goal == "" || input.Kind == "" {
		return nil, DecideOutput{}, errors.New("request_id, action_id, goal, and kind are required")
	}
	if len(input.Choices) == 0 {
		return nil, DecideOutput{}, errors.New("at least one bounded choice is required")
	}

	sourceAgent := input.SourceAgent
	if sourceAgent == "" {
		sourceAgent = "codex"
	}
	allowedTargets := make([]string, 0, len(input.Choices))
	choices := make([]decisionplane.Choice, 0, len(input.Choices))
	for _, choice := range input.Choices {
		allowedTargets = append(allowedTargets, choice.Target)
		choices = append(choices, decisionplane.Choice{
			ID:     choice.ID,
			Target: choice.Target,
			Label:  choice.Label,
		})
	}

	packet := metro.NewPacket(
		input.ActionID,
		sourceAgent,
		input.Goal,
		metro.Action{Kind: input.Kind, Inputs: input.Inputs},
		allowedTargets,
	)
	packet.Constraints.TimeoutMS = input.TimeoutMS
	packet.Constraints.SideEffect = input.SideEffect

	request, err := decisionplane.NewRequest(packet, input.RequestID, input.State, choices)
	if err != nil {
		return nil, DecideOutput{}, err
	}

	provider := runtime.provider
	providerID := runtime.providerID
	if provider == nil {
		if len(input.Scores) == 0 {
			return nil, DecideOutput{}, errors.New("scores are required in static-proof mode")
		}
		provider = decisionplane.StaticProvider{ID: providerID, Scores: input.Scores}
	}

	decision, err := provider.Decide(ctx, request)
	if err != nil {
		return nil, DecideOutput{}, err
	}
	gate, err := decisionplane.ApplyDecision(packet, request, decision, decisionplane.DefaultGatePolicy())
	if err != nil {
		return nil, DecideOutput{}, err
	}

	selectedTarget := ""
	if gate.Route != nil {
		selectedTarget = gate.Route.SelectedTarget
	}
	return nil, DecideOutput{
		RequestID:      request.RequestID,
		ActionID:       request.ActionID,
		ProviderID:     decision.ProviderID,
		ProviderMode:   runtime.providerMode,
		Disposition:    gate.Disposition,
		ReasonCode:     gate.ReasonCode,
		SelectedTarget: selectedTarget,
		Confidence:     gate.Confidence,
		TopMargin:      gate.TopMargin,
		PacketHash:     request.PacketHash,
		StateHash:      request.StateHash,
		ChoicesHash:    request.ChoicesHash,
		Route:          gate.Route,
	}, nil
}

func (runtime *Runtime) verify(_ context.Context, _ *mcp.CallToolRequest, input VerifyInput) (*mcp.CallToolResult, VerifyOutput, error) {
	output := VerifyOutput{
		ActionID:   input.Packet.ActionID,
		RouteID:    input.Route.RouteID,
		ExecutorID: input.Receipt.ExecutorID,
		Status:     input.Receipt.Status,
	}
	if input.Packet.Protocol != metro.PacketProtocol {
		output.Reason = "unsupported packet protocol"
		return nil, output, nil
	}
	if input.Route.Protocol != metro.RouteProtocol {
		output.Reason = "unsupported route protocol"
		return nil, output, nil
	}
	if input.Receipt.Protocol != metro.ReceiptProtocol {
		output.Reason = "unsupported receipt protocol"
		return nil, output, nil
	}
	if input.Receipt.HashAlgorithm != "sha256" {
		output.Reason = "unsupported receipt hash algorithm"
		return nil, output, nil
	}
	if input.Receipt.Status != "SUCCEEDED" {
		output.Reason = "receipt status is not SUCCEEDED; completion remains unverified"
		return nil, output, nil
	}
	if err := metro.Verify(input.Packet, input.Route, input.Result, input.Receipt); err != nil {
		output.Reason = err.Error()
		return nil, output, nil
	}
	output.Verified = true
	return nil, output, nil
}

func (runtime *Runtime) status(_ context.Context, _ *mcp.CallToolRequest, _ StatusInput) (*mcp.CallToolResult, StatusOutput, error) {
	return nil, StatusOutput{
		Version:              Version,
		ProviderMode:         runtime.providerMode,
		ProviderID:           runtime.providerID,
		Tools:                []string{"liminal_decide", "liminal_verify", "liminal_status"},
		DecisionBinding:      "request_id + action_id + packet_hash + state_hash + choices_hash",
		SideEffectPolicy:     "side_effect=true -> REQUIRE_APPROVAL",
		CompletionPolicy:     "only SUCCEEDED receipt with matching packet/route/result hashes verifies completion",
		Transport:            "MCP Streamable HTTP",
		PublicDeploymentNote: "local development binds to localhost; public plugin distribution requires a deployed HTTPS MCP endpoint",
	}, nil
}

func durationFromEnvMillis(name string) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer number of milliseconds", name)
	}
	return time.Duration(value) * time.Millisecond, nil
}

func int64FromEnv(name string) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

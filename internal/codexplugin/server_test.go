package codexplugin

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/safal207/Liminal-Rail-Metro/internal/codingworkflow"
	"github.com/safal207/Liminal-Rail-Metro/internal/decisionplane"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

func TestStaticDecideAutoRoutesBoundedChoice(t *testing.T) {
	runtime, err := NewRuntime(Config{})
	if err != nil {
		t.Fatal(err)
	}

	_, output, err := runtime.decide(context.Background(), nil, DecideInput{
		RequestID: "codex-decision-1",
		ActionID:  "codex-action-1",
		Goal:      "Implement one bounded patch.",
		Kind:      "code.change",
		State:     map[string]any{"stage": "implementation"},
		Choices: []ChoiceInput{
			{ID: "research", Target: "research-agent"},
			{ID: "code", Target: "code-agent"},
			{ID: "qa", Target: "qa-agent"},
		},
		Scores: map[string]float64{
			"research": 1,
			"code":     198,
			"qa":       1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if output.Disposition != decisionplane.DispositionAutoRoute {
		t.Fatalf("expected AUTO_ROUTE, got %#v", output)
	}
	if output.SelectedTarget != "code-agent" || output.Route == nil {
		t.Fatalf("expected code-agent route, got %#v", output)
	}
	if output.PacketHash == "" || output.StateHash == "" || output.ChoicesHash == "" {
		t.Fatalf("expected all provenance hashes, got %#v", output)
	}
}

func TestStaticDecideSideEffectRequiresApproval(t *testing.T) {
	runtime, err := NewRuntime(Config{})
	if err != nil {
		t.Fatal(err)
	}

	_, output, err := runtime.decide(context.Background(), nil, DecideInput{
		RequestID:  "codex-decision-effect",
		ActionID:   "codex-action-effect",
		Goal:       "Deploy one bounded patch.",
		Kind:       "deploy.production",
		SideEffect: true,
		Choices: []ChoiceInput{
			{ID: "deploy", Target: "deploy-agent"},
			{ID: "qa", Target: "qa-agent"},
		},
		Scores: map[string]float64{
			"deploy": 99,
			"qa":     1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if output.Disposition != decisionplane.DispositionRequireApproval {
		t.Fatalf("expected REQUIRE_APPROVAL, got %#v", output)
	}
	if output.Route != nil || output.SelectedTarget != "" {
		t.Fatalf("side-effect decision must not produce a route: %#v", output)
	}
}

func TestVerifyAcceptsBoundSuccessReceipt(t *testing.T) {
	runtime, err := NewRuntime(Config{})
	if err != nil {
		t.Fatal(err)
	}

	packet := metro.NewPacket(
		"verify-action",
		"codex",
		"Verify one result.",
		metro.Action{Kind: "qa.verify", Inputs: map[string]any{"scope": "one"}},
		[]string{"qa-agent"},
	)
	route := metro.Route{
		Protocol:       metro.RouteProtocol,
		RouteID:        "route-verify-action",
		ActionID:       packet.ActionID,
		RouterID:       "proof",
		DecisionMode:   "deterministic",
		SelectedTarget: "qa-agent",
		DecidedAt:      metro.NowISO(),
	}
	result := map[string]any{"passed": true}
	receipt, err := metro.MakeSuccessReceipt(packet, route, result, "artifact://verify")
	if err != nil {
		t.Fatal(err)
	}

	_, output, err := runtime.verify(context.Background(), nil, VerifyInput{
		Packet: packet, Route: route, Result: result, Receipt: receipt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !output.Verified {
		t.Fatalf("expected verified receipt, got %#v", output)
	}
}

func TestVerifyPreservesUnknownAsUnverified(t *testing.T) {
	runtime, err := NewRuntime(Config{})
	if err != nil {
		t.Fatal(err)
	}

	packet := metro.NewPacket(
		"unknown-action",
		"codex",
		"Preserve uncertainty.",
		metro.Action{Kind: "external.effect", Inputs: map[string]any{"scope": "one"}},
		[]string{"effect-agent"},
	)
	route := metro.Route{
		Protocol:       metro.RouteProtocol,
		RouteID:        "route-unknown-action",
		ActionID:       packet.ActionID,
		RouterID:       "proof",
		DecisionMode:   "deterministic",
		SelectedTarget: "effect-agent",
		DecidedAt:      metro.NowISO(),
	}
	result := map[string]any{"accepted": true}
	receipt, err := metro.MakeSuccessReceipt(packet, route, result, "artifact://unknown")
	if err != nil {
		t.Fatal(err)
	}
	receipt.Status = "UNKNOWN"

	_, output, err := runtime.verify(context.Background(), nil, VerifyInput{
		Packet: packet, Route: route, Result: result, Receipt: receipt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if output.Verified {
		t.Fatalf("UNKNOWN must remain unverified: %#v", output)
	}
}

func TestMCPStreamableHTTPExposesStatusAndDecide(t *testing.T) {
	runtime, err := NewRuntime(Config{})
	if err != nil {
		t.Fatal(err)
	}
	server := runtime.NewMCPServer()
	httpServer := httptest.NewServer(MCPHandler(server))
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "codex-plugin-test", Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             httpServer.URL,
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	statusResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "liminal_status",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if statusResult.IsError {
		t.Fatalf("liminal_status returned tool error: %#v", statusResult.Content)
	}

	decideResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "liminal_decide",
		Arguments: map[string]any{
			"request_id": "mcp-decision-1",
			"action_id":  "mcp-action-1",
			"goal":       "Choose the bounded implementation line.",
			"kind":       "code.change",
			"state": map[string]any{
				"stage": "implementation",
			},
			"choices": []map[string]any{
				{"id": "research", "target": "research-agent"},
				{"id": "code", "target": "code-agent"},
				{"id": "qa", "target": "qa-agent"},
			},
			"scores": map[string]any{
				"research": 1,
				"code":     198,
				"qa":       1,
			},
			"side_effect": false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if decideResult.IsError {
		t.Fatalf("liminal_decide returned tool error: %#v", decideResult.Content)
	}
	payload, err := json.Marshal(decideResult.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var output DecideOutput
	if err := json.Unmarshal(payload, &output); err != nil {
		t.Fatal(err)
	}
	if output.Disposition != decisionplane.DispositionAutoRoute || output.SelectedTarget != "code-agent" {
		t.Fatalf("unexpected MCP decision output: %#v", output)
	}
}

type fakeCodingGitHub struct{}

func (fakeCodingGitHub) GetIssue(context.Context, string, int) (codingworkflow.GitHubIssue, error) {
	return codingworkflow.GitHubIssue{
		Number:  16,
		State:   "open",
		Title:   "Codex Plugin v0.3",
		Body:    "Build bounded coding workflow.",
		HTMLURL: "https://github.com/safal207/Liminal-Rail-Metro/issues/16",
	}, nil
}

func (fakeCodingGitHub) GetPullRequest(context.Context, string, int) (codingworkflow.GitHubPullRequest, error) {
	var pull codingworkflow.GitHubPullRequest
	pull.Number = 17
	pull.State = "open"
	pull.HTMLURL = "https://github.com/safal207/Liminal-Rail-Metro/pull/17"
	pull.ChangedFiles = 2
	pull.Base.SHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	pull.Base.Repo.FullName = "safal207/Liminal-Rail-Metro"
	pull.Head.SHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	pull.Head.Repo.FullName = "safal207/Liminal-Rail-Metro"
	return pull, nil
}

func (fakeCodingGitHub) GetPullRequestFiles(context.Context, string, int) ([]codingworkflow.GitHubPRFile, error) {
	return []codingworkflow.GitHubPRFile{
		{SHA: "blob-a", Filename: "internal/codingworkflow/workflow.go", Status: "added", Changes: 10},
		{SHA: "blob-b", Filename: "plugins/liminal-rail-metro/README.md", Status: "modified", Changes: 4},
	}, nil
}

func (fakeCodingGitHub) GetCheckRuns(context.Context, string, string) ([]codingworkflow.GitHubCheckRun, error) {
	return []codingworkflow.GitHubCheckRun{
		{
			ID:          200,
			Name:        "coding-unit",
			HeadSHA:     "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Status:      "completed",
			Conclusion:  "success",
			CompletedAt: "2026-09-19T15:00:00Z",
		},
	}, nil
}

func TestMCPStreamableHTTPCodingWorkflow(t *testing.T) {
	seed := bytes.Repeat([]byte{9}, ed25519.SeedSize)
	signer, err := codingworkflow.NewSigner("liminal-test", ed25519.NewKeyFromSeed(seed))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(Config{
		GitHubClient: fakeCodingGitHub{},
		CodingSigner: signer,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := runtime.NewMCPServer()
	httpServer := httptest.NewServer(MCPHandler(server))
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "coding-workflow-test", Version: "v0.3.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             httpServer.URL,
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	startResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "liminal_coding_start",
		Arguments: map[string]any{
			"workflow_id":  "workflow-16",
			"request_id":   "request-16",
			"action_id":    "action-16",
			"repository":   "safal207/Liminal-Rail-Metro",
			"issue_number": 16,
			"base_sha":     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"allowed_path_prefixes": []string{
				"internal/codingworkflow",
				"plugins/liminal-rail-metro",
			},
			"required_checks": []string{"coding-unit"},
			"choices": []map[string]any{
				{"id": "code", "target": "code-agent"},
				{"id": "qa", "target": "qa-agent"},
			},
			"scores": map[string]any{"code": 99, "qa": 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if startResult.IsError {
		t.Fatalf("coding start returned tool error: %#v", startResult.Content)
	}
	payload, err := json.Marshal(startResult.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var started CodingStartOutput
	if err := json.Unmarshal(payload, &started); err != nil {
		t.Fatal(err)
	}
	if started.Status != "AUTHORIZED" || started.Contract == nil {
		t.Fatalf("unexpected coding start: %#v", started)
	}
	if err := signer.VerifyContract(*started.Contract); err != nil {
		t.Fatal(err)
	}

	verifyResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "liminal_coding_verify",
		Arguments: map[string]any{
			"contract":            started.Contract,
			"pull_request_number": 17,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if verifyResult.IsError {
		t.Fatalf("coding verify returned tool error: %#v", verifyResult.Content)
	}
	payload, err = json.Marshal(verifyResult.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var verification codingworkflow.Verification
	if err := json.Unmarshal(payload, &verification); err != nil {
		t.Fatal(err)
	}
	if verification.Status != codingworkflow.StatusVerified || verification.Receipt == nil {
		t.Fatalf("expected VERIFIED coding receipt, got %#v", verification)
	}
	if err := signer.VerifyReceipt(*verification.Receipt); err != nil {
		t.Fatal(err)
	}
}

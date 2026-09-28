package main

import (
	"context"
	"errors"

	"github.com/safal207/Liminal-Rail-Metro/integrations/moltbook"
	"github.com/safal207/Liminal-Rail-Metro/internal/decisionplane"
	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const (
	controlFixtureRef = "fixture://moltbook-smoke/metro-receipt-v1"
	smokeTarget       = "local-control-fixture"
)

type controlFixture struct {
	Packet  metro.Packet   `json:"packet"`
	Route   metro.Route    `json:"route"`
	Result  map[string]any `json:"result"`
	Receipt metro.Receipt  `json:"receipt"`
}

func newControlFixture() (controlFixture, error) {
	const timestamp = "2026-09-20T00:00:00Z"
	p := metro.NewPacket("molt-control-001", "public-control-fixture", "check local receipt integrity",
		metro.Action{Kind: "fixture.check", Inputs: map[string]any{"fixture": "moltbook-control-v1"}}, []string{"local-fixture"})
	p.CreatedAt = timestamp
	route, err := (metro.Router{ID: "control-router", Policy: map[string]string{"fixture.check": "local-fixture"}}).Route(p)
	if err != nil {
		return controlFixture{}, err
	}
	route.DecidedAt = timestamp
	result := map[string]any{"fixture": "moltbook-control-v1", "integrity": "intact"}
	receipt, err := metro.MakeSuccessReceipt(p, route, result, controlFixtureRef)
	if err != nil {
		return controlFixture{}, err
	}
	receipt.StartedAt, receipt.CompletedAt = timestamp, timestamp
	return controlFixture{Packet: p, Route: route, Result: result, Receipt: receipt}, nil
}

type fixtureVerifier struct {
	proof controlFixture
	calls int
}

func (v *fixtureVerifier) VerifyEvidence(ref string) (map[string]any, error) {
	v.calls++
	if ref != controlFixtureRef {
		return nil, errors.New("only the local control fixture is allowed")
	}
	if err := metro.Verify(v.proof.Packet, v.proof.Route, v.proof.Result, v.proof.Receipt); err != nil {
		return nil, errors.New("control fixture integrity check failed")
	}
	hash, err := metro.HashJSON(v.proof)
	if err != nil {
		return nil, err
	}
	return map[string]any{"verdict": "VERIFIED", "evidence_sha256": hash, "backend": "metro.Verify/local-control-fixture"}, nil
}

type smokeAuthority struct {
	gate  moltbook.DecisionPlaneAuthority
	calls int
}

func newSmokeAuthority() (*smokeAuthority, error) {
	gate, err := moltbook.NewDecisionPlaneAuthority(decisionplane.StaticProvider{
		ID: "moltbook-smoke-local-authority", Scores: map[string]float64{"moltbook-target": 1},
	})
	gate.Policy.PolicyRef = "policy://moltbook/smoke/read-only-control-fixture-v1"
	return &smokeAuthority{gate: gate}, err
}

func (a *smokeAuthority) Authorize(ctx context.Context, packet metro.Packet, target string) (moltbook.AuthorityDecision, error) {
	a.calls++
	if !allowedSmokePacket(packet, target) {
		return moltbook.AuthorityDecision{}, errors.New("smoke authority only permits the read-only control fixture")
	}
	return a.gate.Authorize(ctx, packet, target)
}

func allowedSmokePacket(packet metro.Packet, target string) bool {
	return target == smokeTarget && !packet.Constraints.SideEffect && packet.Action.Kind == "moltbook.verify_evidence" &&
		len(packet.AllowedTargets) == 1 && packet.AllowedTargets[0] == target &&
		packet.Action.Inputs["intent"] == moltbook.IntentVerifyEvidence && packet.Action.Inputs["evidence_ref"] == controlFixtureRef
}

package mirror

import (
	"errors"
	"fmt"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const (
	ReplayReportProtocol   = "mirror.replay-report.v0.1"
	ReplayStatusReproduced = "REPRODUCED"
	ReplayStatusRejected   = "REJECTED"
)

// ReplayCheck records one independently repeated verification step.
type ReplayCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
}

// ReplayReport records whether the verification claimed by a ProofEnvelope can
// be independently reproduced from the original Metro artifacts and authority
// policy.
type ReplayReport struct {
	Protocol        string        `json:"protocol"`
	Status          string        `json:"status"`
	ClaimID         string        `json:"claim_id"`
	ActionID        string        `json:"action_id"`
	Checks          []ReplayCheck `json:"checks"`
	RejectionReason string        `json:"rejection_reason,omitempty"`
}

// ReplayVerifier independently reconstructs the verification path represented
// by a ProofEnvelope. It does not trust the envelope's VERIFIED label by itself.
type ReplayVerifier struct{}

func (ReplayVerifier) Verify(
	envelope ProofEnvelope,
	packet metro.Packet,
	route metro.Route,
	result map[string]any,
	receipt metro.Receipt,
	authority AuthorityPolicy,
) (ReplayReport, error) {
	report := ReplayReport{
		Protocol: ReplayReportProtocol,
		Status:   ReplayStatusRejected,
		ClaimID:  envelope.Claim.ID,
		ActionID: envelope.Claim.ActionID,
		Checks:   make([]ReplayCheck, 0, 8),
	}

	pass := func(name string) {
		report.Checks = append(report.Checks, ReplayCheck{Name: name, Passed: true})
	}
	reject := func(name string, err error) (ReplayReport, error) {
		report.Checks = append(report.Checks, ReplayCheck{Name: name, Passed: false})
		report.RejectionReason = err.Error()
		return report, err
	}

	if err := envelope.Validate(); err != nil {
		return reject("envelope.validate", fmt.Errorf("replay envelope validation: %w", err))
	}
	pass("envelope.validate")

	if packet.Protocol != metro.PacketProtocol {
		return reject("source.protocols", fmt.Errorf("unexpected packet protocol %q", packet.Protocol))
	}
	if route.Protocol != metro.RouteProtocol {
		return reject("source.protocols", fmt.Errorf("unexpected route protocol %q", route.Protocol))
	}
	if receipt.Protocol != metro.ReceiptProtocol {
		return reject("source.protocols", fmt.Errorf("unexpected receipt protocol %q", receipt.Protocol))
	}
	pass("source.protocols")

	if packet.ActionID != envelope.Claim.ActionID {
		return reject("packet.binding", errors.New("packet action_id does not match envelope claim"))
	}
	if packet.Action.Kind != envelope.ActionKind {
		return reject("packet.binding", errors.New("packet action kind does not match envelope"))
	}
	pass("packet.binding")

	if route.ActionID != packet.ActionID {
		return reject("route.binding", errors.New("route action_id does not match packet action_id"))
	}
	if route.RouteID != envelope.Receipt.RouteID {
		return reject("route.binding", errors.New("route_id does not match envelope receipt"))
	}
	if route.SelectedTarget != envelope.Receipt.ExecutorID {
		return reject("route.binding", errors.New("route selected target does not match envelope executor"))
	}
	pass("route.binding")

	if err := matchReceiptProof(receipt, envelope.Receipt); err != nil {
		return reject("receipt.binding", err)
	}
	pass("receipt.binding")

	if err := metro.Verify(packet, route, result, receipt); err != nil {
		return reject("metro.verify", fmt.Errorf("replay metro verification: %w", err))
	}
	pass("metro.verify")

	resultHash, err := HashValue(result)
	if err != nil {
		return reject("claim.result", fmt.Errorf("hash replay result: %w", err))
	}
	if resultHash != envelope.Claim.ValueHash {
		return reject("claim.result", errors.New("replayed result hash does not match envelope claim"))
	}
	pass("claim.result")

	authorityProof, err := authority.AuthorizeProof(packet.Action.Kind, receipt.ExecutorID)
	if err != nil {
		return reject("authority.replay", fmt.Errorf("replay authority verification: %w", err))
	}
	if err := matchAuthorityProof(authorityProof, envelope.Authority); err != nil {
		return reject("authority.replay", err)
	}
	pass("authority.replay")

	report.Status = ReplayStatusReproduced
	report.RejectionReason = ""
	return report, nil
}

func matchReceiptProof(receipt metro.Receipt, proof ReceiptProof) error {
	if receipt.Protocol != proof.Protocol ||
		receipt.ReceiptID != proof.ReceiptID ||
		receipt.ActionID != proof.ActionID ||
		receipt.RouteID != proof.RouteID ||
		receipt.ExecutorID != proof.ExecutorID ||
		receipt.Status != proof.Status ||
		receipt.InputHash != proof.InputHash ||
		receipt.ResultHash != proof.ResultHash ||
		receipt.ResultRef != proof.ResultRef {
		return errors.New("original receipt does not match structured envelope receipt")
	}
	return nil
}

func matchAuthorityProof(replayed, reported AuthorityProof) error {
	if replayed.PolicyID != reported.PolicyID ||
		replayed.PolicyRef != reported.PolicyRef ||
		replayed.PolicyHash != reported.PolicyHash ||
		replayed.ActionKind != reported.ActionKind ||
		replayed.ExecutorID != reported.ExecutorID {
		return errors.New("replayed authority proof does not match envelope authority")
	}
	return nil
}

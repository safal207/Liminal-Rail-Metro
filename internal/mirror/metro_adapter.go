package mirror

import (
	"errors"
	"fmt"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

// EvidenceFromVerifiedReceipt promotes a Metro receipt into external mirror
// evidence only after the receipt has been verified against its packet, route,
// concrete result, claim, and an explicit executor authority policy.
//
// The adapter is intentionally fail-closed. A receipt must:
//   - be bound to the same action as the claim,
//   - use the expected receipt protocol,
//   - represent a successful execution,
//   - carry a durable/non-empty result reference,
//   - pass metro.Verify(...),
//   - bind the verified result hash to the claim value hash,
//   - come from an executor explicitly authoritative for packet.Action.Kind,
//   - and carry a durable authority policy reference + canonical policy hash.
//
// Only then can it cross the mirror boundary as SourceExternal evidence.
func EvidenceFromVerifiedReceipt(
	claim Claim,
	packet metro.Packet,
	route metro.Route,
	result map[string]any,
	receipt metro.Receipt,
	authority AuthorityPolicy,
) (Evidence, error) {
	if claim.ID == "" || claim.ActionID == "" || claim.ValueHash == "" {
		return Evidence{}, errors.New("invalid claim: id, action_id and value_hash are required")
	}
	if claim.ActionID != packet.ActionID {
		return Evidence{}, errors.New("claim action_id does not match packet action_id")
	}
	if receipt.Protocol != metro.ReceiptProtocol {
		return Evidence{}, fmt.Errorf("unexpected receipt protocol %q", receipt.Protocol)
	}
	if receipt.Status != "SUCCEEDED" {
		return Evidence{}, fmt.Errorf("receipt status %q is not promotable", receipt.Status)
	}
	if receipt.ReceiptID == "" {
		return Evidence{}, errors.New("receipt_id is required for provenance")
	}
	if receipt.ResultRef == "" {
		return Evidence{}, errors.New("result_ref is required for external provenance")
	}

	// Verify structural and cryptographic bindings before consulting executor
	// metadata for authority. An unverified receipt cannot carry authority.
	if err := metro.Verify(packet, route, result, receipt); err != nil {
		return Evidence{}, fmt.Errorf("verify metro receipt: %w", err)
	}
	if receipt.ActionID != claim.ActionID {
		return Evidence{}, errors.New("verified receipt is not bound to claim action_id")
	}
	if receipt.ResultHash != claim.ValueHash {
		return Evidence{}, errors.New("verified receipt result hash does not match claim value hash")
	}

	authorityProof, err := authority.AuthorizeProof(packet.Action.Kind, receipt.ExecutorID)
	if err != nil {
		return Evidence{}, fmt.Errorf("external proof authority rejected: %w", err)
	}

	return Evidence{
		ClaimID:   claim.ID,
		ActionID:  claim.ActionID,
		Source:    SourceExternal,
		ValueHash: receipt.ResultHash,
		Verified:  true,
		Provenance: []string{
			"receipt://" + receipt.ReceiptID,
			"route://" + receipt.RouteID,
			"executor://" + receipt.ExecutorID,
			authorityProof.DecisionRef(),
			authorityProof.PolicyRef,
			"policy-sha256://" + authorityProof.PolicyHash,
			receipt.ResultRef,
		},
	}, nil
}

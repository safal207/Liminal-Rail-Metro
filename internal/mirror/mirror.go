package mirror

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Source identifies where a claim or piece of evidence originated.
// Mirror sources are intentionally distinct from external evidence so that
// internally generated agreement can never satisfy an external-proof gate.
type Source string

const (
	SourceAgent        Source = "agent"
	SourceMirrorLocal  Source = "mirror.local"
	SourceMirrorDomain Source = "mirror.domain"
	SourceMirrorSystem Source = "mirror.system"
	SourceExternal     Source = "external"
)

// Claim is the state transition an agent wants to commit.
// ValueHash binds the claim to a specific value without requiring the gate to
// understand the value's domain-specific shape.
type Claim struct {
	ID           string `json:"id"`
	ActionID     string `json:"action_id"`
	ValueHash    string `json:"value_hash"`
	Irreversible bool   `json:"irreversible"`
}

// Evidence represents an observation about a claim.
// Verified is meaningful only together with SourceExternal and Provenance.
// Mirror evidence may be useful for divergence detection, but never counts as
// external proof.
type Evidence struct {
	ClaimID    string   `json:"claim_id"`
	ActionID   string   `json:"action_id"`
	Source     Source   `json:"source"`
	ValueHash  string   `json:"value_hash"`
	Verified   bool     `json:"verified"`
	Provenance []string `json:"provenance,omitempty"`
}

// Decision is the fail-closed result of evaluating a claim against evidence.
type Decision struct {
	Commit           bool   `json:"commit"`
	Reason           string `json:"reason"`
	Divergent        bool   `json:"divergent"`
	VerifiedExternal int    `json:"verified_external"`
}

// Gate enforces the mirror boundary:
//
//   belief != reflection != evidence != verified state
//
// A commit requires at least one matching, verified external proof with
// provenance. For irreversible claims, any bound evidence that disagrees with
// the claimed value freezes the commit.
type Gate struct{}

func (Gate) Evaluate(claim Claim, evidence []Evidence) Decision {
	if claim.ID == "" || claim.ActionID == "" || claim.ValueHash == "" {
		return Decision{Reason: "invalid claim: id, action_id and value_hash are required"}
	}

	divergent := false
	verifiedExternal := 0

	for _, item := range evidence {
		// Evidence for another claim/action cannot authorize this commit.
		if item.ClaimID != claim.ID || item.ActionID != claim.ActionID {
			continue
		}

		if item.ValueHash != "" && item.ValueHash != claim.ValueHash {
			divergent = true
		}

		if item.Source == SourceExternal &&
			item.Verified &&
			item.ValueHash == claim.ValueHash &&
			hasProvenance(item.Provenance) {
			verifiedExternal++
		}
	}

	if claim.Irreversible && divergent {
		return Decision{
			Reason:           "divergent evidence freezes irreversible effect",
			Divergent:        true,
			VerifiedExternal: verifiedExternal,
		}
	}

	if verifiedExternal == 0 {
		return Decision{
			Reason:           "verified external proof with provenance required",
			Divergent:        divergent,
			VerifiedExternal: 0,
		}
	}

	return Decision{
		Commit:           true,
		Reason:           "verified external proof accepted",
		Divergent:        divergent,
		VerifiedExternal: verifiedExternal,
	}
}

// HashValue returns a stable SHA-256 hash of a JSON-serializable value.
func HashValue(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func hasProvenance(values []string) bool {
	for _, value := range values {
		if value != "" {
			return true
		}
	}
	return false
}

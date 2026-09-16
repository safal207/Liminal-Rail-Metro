package mirror

import (
	"errors"
	"fmt"
)

// AuthorityPolicy defines which executor identities are authoritative for a
// specific action/effect kind at the external-proof boundary.
//
// It is intentionally exact-match and deny-by-default. Routing permission and
// receipt validity do not imply proof authority.
type AuthorityPolicy struct {
	ID                 string              `json:"id"`
	ExecutorsByAction  map[string][]string `json:"executors_by_action"`
}

// Authorize returns nil only when executorID is explicitly allowed to certify
// the supplied action kind under this policy.
func (p AuthorityPolicy) Authorize(actionKind, executorID string) error {
	if p.ID == "" {
		return errors.New("authority policy id is required")
	}
	if actionKind == "" {
		return errors.New("action kind is required for authority check")
	}
	if executorID == "" {
		return errors.New("executor id is required for authority check")
	}

	allowed, ok := p.ExecutorsByAction[actionKind]
	if !ok || len(allowed) == 0 {
		return fmt.Errorf("no authoritative executor configured for action kind %q", actionKind)
	}

	for _, candidate := range allowed {
		if candidate == executorID {
			return nil
		}
	}

	return fmt.Errorf("executor %q is not authoritative for action kind %q", executorID, actionKind)
}

// Ref returns a compact provenance reference for a successful authority check.
func (p AuthorityPolicy) Ref(actionKind, executorID string) string {
	return fmt.Sprintf("authority://%s/%s/%s", p.ID, actionKind, executorID)
}

package adaptive

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const AuthorityProtocol = "liminal.adaptive.authority.v0.1"

// AuthorityGrant is a content-addressed, epoch-scoped allow-list.
// It is not a signature or remote attestation. The caller is responsible for
// supplying a trusted grant; the learner guarantees it will not silently
// learn outside the supplied grant.
type AuthorityGrant struct {
	Protocol       string   `json:"protocol"`
	AuthorityID    string   `json:"authority_id"`
	Epoch          string   `json:"epoch"`
	AllowedActions []string `json:"allowed_actions"`
	AuthorityHash  string   `json:"authority_hash"`
}

type authorityDigestMaterial struct {
	Protocol       string   `json:"protocol"`
	AuthorityID    string   `json:"authority_id"`
	Epoch          string   `json:"epoch"`
	AllowedActions []string `json:"allowed_actions"`
}

func NewAuthorityGrant(authorityID, epoch string, actions []string) (AuthorityGrant, error) {
	if authorityID == "" || epoch == "" {
		return AuthorityGrant{}, errors.New("authority_id and epoch are required")
	}
	canonical, err := canonicalActions(actions)
	if err != nil {
		return AuthorityGrant{}, err
	}
	grant := AuthorityGrant{
		Protocol:       AuthorityProtocol,
		AuthorityID:    authorityID,
		Epoch:          epoch,
		AllowedActions: canonical,
	}
	grant.AuthorityHash, err = authorityHash(grant)
	if err != nil {
		return AuthorityGrant{}, err
	}
	return grant, nil
}

func (g AuthorityGrant) Validate() error {
	if g.Protocol != AuthorityProtocol {
		return fmt.Errorf("unsupported authority protocol %q", g.Protocol)
	}
	if g.AuthorityID == "" || g.Epoch == "" {
		return errors.New("authority_id and epoch are required")
	}
	canonical, err := canonicalActions(g.AllowedActions)
	if err != nil {
		return err
	}
	for i := range canonical {
		if canonical[i] != g.AllowedActions[i] {
			return errors.New("allowed_actions must use canonical lexical order")
		}
	}
	if !isSHA256Hex(g.AuthorityHash) {
		return errors.New("authority_hash must be a lowercase sha256 hex digest")
	}
	expected, err := authorityHash(g)
	if err != nil {
		return err
	}
	if expected != g.AuthorityHash {
		return errors.New("authority_hash does not match authority content")
	}
	return nil
}

func (g AuthorityGrant) Allows(action string) bool {
	if action == "" {
		return false
	}
	i := sort.SearchStrings(g.AllowedActions, action)
	return i < len(g.AllowedActions) && g.AllowedActions[i] == action
}

func (g AuthorityGrant) Ref() string {
	return "adaptive-authority://sha256/" + g.AuthorityHash
}

func authorityHash(g AuthorityGrant) (string, error) {
	return metro.HashJSON(authorityDigestMaterial{
		Protocol:       g.Protocol,
		AuthorityID:    g.AuthorityID,
		Epoch:          g.Epoch,
		AllowedActions: append([]string(nil), g.AllowedActions...),
	})
}

func canonicalActions(actions []string) ([]string, error) {
	if len(actions) == 0 {
		return nil, errors.New("authority requires at least one allowed action")
	}
	out := append([]string(nil), actions...)
	sort.Strings(out)
	for i, action := range out {
		if action == "" {
			return nil, errors.New("authority action must not be empty")
		}
		if i > 0 && out[i-1] == action {
			return nil, errors.New("authority actions must be unique")
		}
	}
	return out, nil
}

// BindAuthorityResult adds the authority proof to the exact measured result
// that Metro later hashes into the receipt.
func BindAuthorityResult(result map[string]any, grant AuthorityGrant) (map[string]any, error) {
	if err := grant.Validate(); err != nil {
		return nil, err
	}
	bound := cloneResult(result)
	bound["authority_protocol"] = grant.Protocol
	bound["authority_id"] = grant.AuthorityID
	bound["authority_epoch"] = grant.Epoch
	bound["authority_hash"] = grant.AuthorityHash
	return bound, nil
}

// AuthorityLearner scopes one durable journal to one immutable authority epoch.
// Rotating the epoch or action set requires an explicit new journal/migration.
type AuthorityLearner struct {
	grant   AuthorityGrant
	durable *DurableLearner
}

func OpenAuthorityLearner(path string, grant AuthorityGrant, exploration float64) (*AuthorityLearner, error) {
	if err := grant.Validate(); err != nil {
		return nil, err
	}
	if err := validateAuthorityJournal(path, grant); err != nil {
		return nil, err
	}
	durable, err := OpenDurableLearner(path, grant.AllowedActions, exploration)
	if err != nil {
		return nil, err
	}
	return &AuthorityLearner{grant: grant, durable: durable}, nil
}

func (l *AuthorityLearner) Grant() AuthorityGrant {
	g := l.grant
	g.AllowedActions = append([]string(nil), l.grant.AllowedActions...)
	return g
}

func (l *AuthorityLearner) Choose(ctx Context) (ContextDecision, error) {
	return l.durable.Choose(ctx)
}

func (l *AuthorityLearner) SnapshotContext(ctx Context) (map[string]ActionStat, error) {
	return l.durable.SnapshotContext(ctx)
}

func (l *AuthorityLearner) BestObserved(ctx Context) (string, ActionStat, bool, error) {
	return l.durable.BestObserved(ctx)
}

func (l *AuthorityLearner) JournalHead() (uint64, string) {
	return l.durable.JournalHead()
}

func (l *AuthorityLearner) Apply(receipt metro.Receipt, result map[string]any, exp Experience) (ApplyResult, error) {
	if err := validateAuthorityEvidence(result, exp, l.grant); err != nil {
		return ApplyResult{}, err
	}
	return l.durable.Apply(receipt, result, exp)
}

func validateAuthorityEvidence(result map[string]any, exp Experience, grant AuthorityGrant) error {
	if !grant.Allows(exp.SelectedAction) {
		return fmt.Errorf("selected action %q is outside authority allow-list", exp.SelectedAction)
	}
	if valueString(result, "selected_action") != exp.SelectedAction {
		return errors.New("authority result selected_action does not match experience")
	}
	if valueString(result, "authority_protocol") != grant.Protocol ||
		valueString(result, "authority_id") != grant.AuthorityID ||
		valueString(result, "authority_epoch") != grant.Epoch ||
		valueString(result, "authority_hash") != grant.AuthorityHash {
		return errors.New("measured result is not bound to the active authority epoch")
	}
	return nil
}

func validateAuthorityJournal(path string, grant AuthorityGrant) error {
	f, err := os.Open(path)
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
			return fmt.Errorf("authority journal line %d is not valid JSON: %w", line, err)
		}
		if err := validateAuthorityEvidence(entry.Result, entry.Experience, grant); err != nil {
			return fmt.Errorf("authority journal line %d rejected: %w", line, err)
		}
	}
	return scanner.Err()
}

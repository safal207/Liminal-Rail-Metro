package adaptive

import (
	"bufio"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const (
	SignedAuthorityProtocol   = "liminal.adaptive.signed-authority.v0.1"
	AuthorityRotationProtocol = "liminal.adaptive.authority-rotation.v0.1"
	AuthorityChainProtocol    = "liminal.adaptive.authority-chain.v0.1"
	SignatureAlgorithmEd25519 = "ed25519"
)

// SignedAuthorityGrant proves possession of an issuer key over one exact
// AuthorityGrant. The signature is not trusted merely because it verifies;
// trust comes from a pinned AuthorityTrustRoot plus an explicit rotation chain.
type SignedAuthorityGrant struct {
	Protocol            string         `json:"protocol"`
	Grant               AuthorityGrant `json:"grant"`
	IssuerID            string         `json:"issuer_id"`
	IssuerKeyID         string         `json:"issuer_key_id"`
	IssuerPublicKey     string         `json:"issuer_public_key_base64"`
	SignatureAlgorithm  string         `json:"signature_algorithm"`
	IssuedAt            string         `json:"issued_at"`
	Signature           string         `json:"signature_base64"`
	SignedAuthorityHash string         `json:"signed_authority_hash"`
}

type signedAuthorityMaterial struct {
	Protocol           string         `json:"protocol"`
	Grant              AuthorityGrant `json:"grant"`
	IssuerID           string         `json:"issuer_id"`
	IssuerKeyID        string         `json:"issuer_key_id"`
	IssuerPublicKey    string         `json:"issuer_public_key_base64"`
	SignatureAlgorithm string         `json:"signature_algorithm"`
	IssuedAt           string         `json:"issued_at"`
}

type signedAuthorityHashMaterial struct {
	Material  signedAuthorityMaterial `json:"material"`
	Signature string                  `json:"signature_base64"`
}

func SignAuthorityGrant(grant AuthorityGrant, issuerID string, privateKey ed25519.PrivateKey) (SignedAuthorityGrant, error) {
	if err := grant.Validate(); err != nil {
		return SignedAuthorityGrant{}, err
	}
	if issuerID == "" {
		return SignedAuthorityGrant{}, errors.New("issuer_id is required")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return SignedAuthorityGrant{}, errors.New("invalid ed25519 private key")
	}
	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok || len(publicKey) != ed25519.PublicKeySize {
		return SignedAuthorityGrant{}, errors.New("invalid ed25519 public key")
	}
	keyID := authorityKeyID(publicKey)
	signed := SignedAuthorityGrant{
		Protocol:           SignedAuthorityProtocol,
		Grant:              grant,
		IssuerID:           issuerID,
		IssuerKeyID:        keyID,
		IssuerPublicKey:    base64.StdEncoding.EncodeToString(publicKey),
		SignatureAlgorithm: SignatureAlgorithmEd25519,
		IssuedAt:           time.Now().UTC().Format(time.RFC3339Nano),
	}
	materialBytes, err := json.Marshal(signed.material())
	if err != nil {
		return SignedAuthorityGrant{}, err
	}
	signed.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, materialBytes))
	signed.SignedAuthorityHash, err = signedAuthorityHash(signed)
	if err != nil {
		return SignedAuthorityGrant{}, err
	}
	return signed, nil
}

func (s SignedAuthorityGrant) material() signedAuthorityMaterial {
	return signedAuthorityMaterial{
		Protocol:           s.Protocol,
		Grant:              s.Grant,
		IssuerID:           s.IssuerID,
		IssuerKeyID:        s.IssuerKeyID,
		IssuerPublicKey:    s.IssuerPublicKey,
		SignatureAlgorithm: s.SignatureAlgorithm,
		IssuedAt:           s.IssuedAt,
	}
}

func (s SignedAuthorityGrant) SelfVerify() error {
	if s.Protocol != SignedAuthorityProtocol {
		return fmt.Errorf("unsupported signed authority protocol %q", s.Protocol)
	}
	if err := s.Grant.Validate(); err != nil {
		return err
	}
	if s.IssuerID == "" || s.IssuerKeyID == "" || s.IssuerPublicKey == "" || s.IssuedAt == "" {
		return errors.New("signed authority requires issuer identity, key, and issued_at")
	}
	if s.SignatureAlgorithm != SignatureAlgorithmEd25519 {
		return fmt.Errorf("unsupported authority signature algorithm %q", s.SignatureAlgorithm)
	}
	publicKey, err := decodeAuthorityPublicKey(s.IssuerPublicKey)
	if err != nil {
		return err
	}
	if authorityKeyID(publicKey) != s.IssuerKeyID {
		return errors.New("issuer_key_id does not match issuer public key")
	}
	signature, err := base64.StdEncoding.DecodeString(s.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("invalid ed25519 authority signature encoding")
	}
	materialBytes, err := json.Marshal(s.material())
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, materialBytes, signature) {
		return errors.New("authority signature verification failed")
	}
	if !isSHA256Hex(s.SignedAuthorityHash) {
		return errors.New("signed_authority_hash must be a lowercase sha256 hex digest")
	}
	expectedHash, err := signedAuthorityHash(s)
	if err != nil {
		return err
	}
	if expectedHash != s.SignedAuthorityHash {
		return errors.New("signed_authority_hash does not match signed authority content")
	}
	return nil
}

func (s SignedAuthorityGrant) Ref() string {
	return "adaptive-signed-authority://sha256/" + s.SignedAuthorityHash
}

func (s SignedAuthorityGrant) PublicKey() (ed25519.PublicKey, error) {
	return decodeAuthorityPublicKey(s.IssuerPublicKey)
}

func signedAuthorityHash(s SignedAuthorityGrant) (string, error) {
	return metro.HashJSON(signedAuthorityHashMaterial{Material: s.material(), Signature: s.Signature})
}

func authorityKeyID(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return hex.EncodeToString(sum[:])
}

func decodeAuthorityPublicKey(encoded string) (ed25519.PublicKey, error) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(decoded) != ed25519.PublicKeySize {
		return nil, errors.New("invalid ed25519 authority public key encoding")
	}
	return ed25519.PublicKey(append([]byte(nil), decoded...)), nil
}

// AuthorityTrustRoot is deliberately small enough to pin in configuration.
// A self-signed grant is trusted only if both its signed authority hash and
// issuer key identity match this root.
type AuthorityTrustRoot struct {
	Protocol                string `json:"protocol"`
	AuthorityID             string `json:"authority_id"`
	RootSignedAuthorityHash string `json:"root_signed_authority_hash"`
	RootAuthorityHash       string `json:"root_authority_hash"`
	IssuerID                string `json:"issuer_id"`
	IssuerKeyID             string `json:"issuer_key_id"`
}

func NewAuthorityTrustRoot(root SignedAuthorityGrant) (AuthorityTrustRoot, error) {
	if err := root.SelfVerify(); err != nil {
		return AuthorityTrustRoot{}, err
	}
	return AuthorityTrustRoot{
		Protocol:                AuthorityChainProtocol,
		AuthorityID:             root.Grant.AuthorityID,
		RootSignedAuthorityHash: root.SignedAuthorityHash,
		RootAuthorityHash:       root.Grant.AuthorityHash,
		IssuerID:                root.IssuerID,
		IssuerKeyID:             root.IssuerKeyID,
	}, nil
}

func (r AuthorityTrustRoot) Validate() error {
	if r.Protocol != AuthorityChainProtocol {
		return fmt.Errorf("unsupported trust-root protocol %q", r.Protocol)
	}
	if r.AuthorityID == "" || r.IssuerID == "" {
		return errors.New("trust root requires authority_id and issuer_id")
	}
	if !isSHA256Hex(r.RootSignedAuthorityHash) || !isSHA256Hex(r.RootAuthorityHash) || !isSHA256Hex(r.IssuerKeyID) {
		return errors.New("trust root requires sha256 authority and key identifiers")
	}
	return nil
}

// AuthorityRotation is signed by the currently trusted issuer and explicitly
// commits to the next authority content and next issuer key identity.
type AuthorityRotation struct {
	Protocol                string `json:"protocol"`
	AuthorityID             string `json:"authority_id"`
	FromEpoch               string `json:"from_epoch"`
	ToEpoch                 string `json:"to_epoch"`
	FromAuthorityHash       string `json:"from_authority_hash"`
	ToAuthorityHash         string `json:"to_authority_hash"`
	FromSignedAuthorityHash string `json:"from_signed_authority_hash"`
	ToSignedAuthorityHash   string `json:"to_signed_authority_hash"`
	SourceJournalSequence   uint64 `json:"source_journal_sequence"`
	SourceJournalHead       string `json:"source_journal_head,omitempty"`
	FromIssuerID            string `json:"from_issuer_id"`
	FromIssuerKeyID         string `json:"from_issuer_key_id"`
	ToIssuerID              string `json:"to_issuer_id"`
	ToIssuerKeyID           string `json:"to_issuer_key_id"`
	IssuedAt                string `json:"issued_at"`
	SignatureAlgorithm      string `json:"signature_algorithm"`
	Signature               string `json:"signature_base64"`
	RotationHash            string `json:"rotation_hash"`
}

type authorityRotationMaterial struct {
	Protocol                string `json:"protocol"`
	AuthorityID             string `json:"authority_id"`
	FromEpoch               string `json:"from_epoch"`
	ToEpoch                 string `json:"to_epoch"`
	FromAuthorityHash       string `json:"from_authority_hash"`
	ToAuthorityHash         string `json:"to_authority_hash"`
	FromSignedAuthorityHash string `json:"from_signed_authority_hash"`
	ToSignedAuthorityHash   string `json:"to_signed_authority_hash"`
	SourceJournalSequence   uint64 `json:"source_journal_sequence"`
	SourceJournalHead       string `json:"source_journal_head,omitempty"`
	FromIssuerID            string `json:"from_issuer_id"`
	FromIssuerKeyID         string `json:"from_issuer_key_id"`
	ToIssuerID              string `json:"to_issuer_id"`
	ToIssuerKeyID           string `json:"to_issuer_key_id"`
	IssuedAt                string `json:"issued_at"`
	SignatureAlgorithm      string `json:"signature_algorithm"`
}

type authorityRotationHashMaterial struct {
	Material  authorityRotationMaterial `json:"material"`
	Signature string                    `json:"signature_base64"`
}

func (r AuthorityRotation) material() authorityRotationMaterial {
	return authorityRotationMaterial{
		Protocol:                r.Protocol,
		AuthorityID:             r.AuthorityID,
		FromEpoch:               r.FromEpoch,
		ToEpoch:                 r.ToEpoch,
		FromAuthorityHash:       r.FromAuthorityHash,
		ToAuthorityHash:         r.ToAuthorityHash,
		FromSignedAuthorityHash: r.FromSignedAuthorityHash,
		ToSignedAuthorityHash:   r.ToSignedAuthorityHash,
		SourceJournalSequence:   r.SourceJournalSequence,
		SourceJournalHead:       r.SourceJournalHead,
		FromIssuerID:            r.FromIssuerID,
		FromIssuerKeyID:         r.FromIssuerKeyID,
		ToIssuerID:              r.ToIssuerID,
		ToIssuerKeyID:           r.ToIssuerKeyID,
		IssuedAt:                r.IssuedAt,
		SignatureAlgorithm:      r.SignatureAlgorithm,
	}
}

func SignAuthorityRotation(current, next SignedAuthorityGrant, sourceSequence uint64, sourceHead string, currentPrivateKey ed25519.PrivateKey) (AuthorityRotation, error) {
	if err := current.SelfVerify(); err != nil {
		return AuthorityRotation{}, err
	}
	if err := next.SelfVerify(); err != nil {
		return AuthorityRotation{}, err
	}
	if current.Grant.AuthorityID != next.Grant.AuthorityID {
		return AuthorityRotation{}, errors.New("authority rotation cannot change authority_id")
	}
	if current.Grant.Epoch == next.Grant.Epoch || current.SignedAuthorityHash == next.SignedAuthorityHash {
		return AuthorityRotation{}, errors.New("authority rotation requires a distinct next epoch and signed authority")
	}
	if err := validateRotationAnchor(sourceSequence, sourceHead); err != nil {
		return AuthorityRotation{}, err
	}
	if len(currentPrivateKey) != ed25519.PrivateKeySize {
		return AuthorityRotation{}, errors.New("invalid current issuer private key")
	}
	currentPublic, ok := currentPrivateKey.Public().(ed25519.PublicKey)
	if !ok || authorityKeyID(currentPublic) != current.IssuerKeyID {
		return AuthorityRotation{}, errors.New("rotation private key does not match current trusted issuer")
	}
	rotation := AuthorityRotation{
		Protocol:                AuthorityRotationProtocol,
		AuthorityID:             current.Grant.AuthorityID,
		FromEpoch:               current.Grant.Epoch,
		ToEpoch:                 next.Grant.Epoch,
		FromAuthorityHash:       current.Grant.AuthorityHash,
		ToAuthorityHash:         next.Grant.AuthorityHash,
		FromSignedAuthorityHash: current.SignedAuthorityHash,
		ToSignedAuthorityHash:   next.SignedAuthorityHash,
		SourceJournalSequence:   sourceSequence,
		SourceJournalHead:       sourceHead,
		FromIssuerID:            current.IssuerID,
		FromIssuerKeyID:         current.IssuerKeyID,
		ToIssuerID:              next.IssuerID,
		ToIssuerKeyID:           next.IssuerKeyID,
		IssuedAt:                time.Now().UTC().Format(time.RFC3339Nano),
		SignatureAlgorithm:      SignatureAlgorithmEd25519,
	}
	materialBytes, err := json.Marshal(rotation.material())
	if err != nil {
		return AuthorityRotation{}, err
	}
	rotation.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(currentPrivateKey, materialBytes))
	rotation.RotationHash, err = authorityRotationHash(rotation)
	if err != nil {
		return AuthorityRotation{}, err
	}
	return rotation, nil
}

func (r AuthorityRotation) Verify(current, next SignedAuthorityGrant) error {
	if err := current.SelfVerify(); err != nil {
		return err
	}
	if err := next.SelfVerify(); err != nil {
		return err
	}
	if r.Protocol != AuthorityRotationProtocol || r.SignatureAlgorithm != SignatureAlgorithmEd25519 {
		return errors.New("unsupported authority rotation protocol or signature algorithm")
	}
	if r.AuthorityID != current.Grant.AuthorityID || r.AuthorityID != next.Grant.AuthorityID ||
		r.FromEpoch != current.Grant.Epoch || r.ToEpoch != next.Grant.Epoch ||
		r.FromAuthorityHash != current.Grant.AuthorityHash || r.ToAuthorityHash != next.Grant.AuthorityHash ||
		r.FromSignedAuthorityHash != current.SignedAuthorityHash || r.ToSignedAuthorityHash != next.SignedAuthorityHash ||
		r.FromIssuerID != current.IssuerID || r.FromIssuerKeyID != current.IssuerKeyID ||
		r.ToIssuerID != next.IssuerID || r.ToIssuerKeyID != next.IssuerKeyID {
		return errors.New("authority rotation does not match the signed authority transition")
	}
	if r.FromEpoch == r.ToEpoch {
		return errors.New("authority rotation requires a new epoch")
	}
	if err := validateRotationAnchor(r.SourceJournalSequence, r.SourceJournalHead); err != nil {
		return err
	}
	publicKey, err := current.PublicKey()
	if err != nil {
		return err
	}
	signature, err := base64.StdEncoding.DecodeString(r.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("invalid authority rotation signature encoding")
	}
	materialBytes, err := json.Marshal(r.material())
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, materialBytes, signature) {
		return errors.New("authority rotation signature verification failed")
	}
	if !isSHA256Hex(r.RotationHash) {
		return errors.New("rotation_hash must be a lowercase sha256 hex digest")
	}
	expectedHash, err := authorityRotationHash(r)
	if err != nil {
		return err
	}
	if expectedHash != r.RotationHash {
		return errors.New("rotation_hash does not match rotation content")
	}
	return nil
}

func (r AuthorityRotation) Ref() string {
	return "adaptive-authority-rotation://sha256/" + r.RotationHash
}

func authorityRotationHash(r AuthorityRotation) (string, error) {
	return metro.HashJSON(authorityRotationHashMaterial{Material: r.material(), Signature: r.Signature})
}

func validateRotationAnchor(sequence uint64, head string) error {
	if sequence == 0 {
		if head != "" {
			return errors.New("empty source journal requires an empty journal head")
		}
		return nil
	}
	if !isSHA256Hex(head) {
		return errors.New("non-empty source journal requires a sha256 journal head")
	}
	return nil
}

// AuthorityChain is a portable proof of authority continuity from a pinned
// trust root to the currently active signed grant.
type AuthorityChain struct {
	Protocol  string                 `json:"protocol"`
	Root      AuthorityTrustRoot     `json:"root"`
	Grants    []SignedAuthorityGrant `json:"grants"`
	Rotations []AuthorityRotation    `json:"rotations"`
}

func NewRootAuthorityChain(rootGrant SignedAuthorityGrant) (AuthorityChain, error) {
	root, err := NewAuthorityTrustRoot(rootGrant)
	if err != nil {
		return AuthorityChain{}, err
	}
	chain := AuthorityChain{Protocol: AuthorityChainProtocol, Root: root, Grants: []SignedAuthorityGrant{rootGrant}}
	if err := chain.Verify(); err != nil {
		return AuthorityChain{}, err
	}
	return chain, nil
}

func (c AuthorityChain) Verify() error {
	if c.Protocol != AuthorityChainProtocol {
		return fmt.Errorf("unsupported authority chain protocol %q", c.Protocol)
	}
	if err := c.Root.Validate(); err != nil {
		return err
	}
	if len(c.Grants) == 0 || len(c.Rotations) != len(c.Grants)-1 {
		return errors.New("authority chain requires one fewer rotation than grants")
	}
	rootGrant := c.Grants[0]
	if err := rootGrant.SelfVerify(); err != nil {
		return err
	}
	if rootGrant.Grant.AuthorityID != c.Root.AuthorityID || rootGrant.Grant.AuthorityHash != c.Root.RootAuthorityHash ||
		rootGrant.SignedAuthorityHash != c.Root.RootSignedAuthorityHash || rootGrant.IssuerID != c.Root.IssuerID || rootGrant.IssuerKeyID != c.Root.IssuerKeyID {
		return errors.New("authority chain root grant does not match pinned trust root")
	}
	for i := range c.Grants {
		if err := c.Grants[i].SelfVerify(); err != nil {
			return fmt.Errorf("authority chain grant %d invalid: %w", i, err)
		}
		if c.Grants[i].Grant.AuthorityID != c.Root.AuthorityID {
			return fmt.Errorf("authority chain grant %d changes authority_id", i)
		}
		if i == 0 {
			continue
		}
		if err := c.Rotations[i-1].Verify(c.Grants[i-1], c.Grants[i]); err != nil {
			return fmt.Errorf("authority chain rotation %d invalid: %w", i-1, err)
		}
	}
	return nil
}

func (c AuthorityChain) Current() (SignedAuthorityGrant, error) {
	if err := c.Verify(); err != nil {
		return SignedAuthorityGrant{}, err
	}
	return c.Grants[len(c.Grants)-1], nil
}

func (c AuthorityChain) LatestRotation() *AuthorityRotation {
	if len(c.Rotations) == 0 {
		return nil
	}
	r := c.Rotations[len(c.Rotations)-1]
	return &r
}

// BindSignedAuthorityResult binds each learned measurement to the exact signed
// grant, not merely to the unsigned v0.4 authority content hash.
func BindSignedAuthorityResult(result map[string]any, signed SignedAuthorityGrant) (map[string]any, error) {
	if err := signed.SelfVerify(); err != nil {
		return nil, err
	}
	bound, err := BindAuthorityResult(result, signed.Grant)
	if err != nil {
		return nil, err
	}
	bound["signed_authority_protocol"] = signed.Protocol
	bound["authority_issuer_id"] = signed.IssuerID
	bound["authority_issuer_key_id"] = signed.IssuerKeyID
	bound["signed_authority_hash"] = signed.SignedAuthorityHash
	return bound, nil
}

func validateSignedAuthorityEvidence(result map[string]any, exp Experience, signed SignedAuthorityGrant) error {
	if err := signed.SelfVerify(); err != nil {
		return err
	}
	if err := validateAuthorityEvidence(result, exp, signed.Grant); err != nil {
		return err
	}
	if valueString(result, "signed_authority_protocol") != signed.Protocol ||
		valueString(result, "authority_issuer_id") != signed.IssuerID ||
		valueString(result, "authority_issuer_key_id") != signed.IssuerKeyID ||
		valueString(result, "signed_authority_hash") != signed.SignedAuthorityHash {
		return errors.New("measured result is not bound to the active signed authority")
	}
	return nil
}

// ChainedAuthorityLearner is the v0.5 entry point. Every open verifies the full
// chain back to the pinned root and every journal row is checked against the
// currently active signed authority.
type ChainedAuthorityLearner struct {
	journalPath string
	chain       AuthorityChain
	learner     *AuthorityLearner
}

func OpenChainedAuthorityLearner(path string, chain AuthorityChain, exploration float64) (*ChainedAuthorityLearner, error) {
	if err := chain.Verify(); err != nil {
		return nil, err
	}
	current, err := chain.Current()
	if err != nil {
		return nil, err
	}
	if err := validateSignedAuthorityJournal(path, current); err != nil {
		return nil, err
	}
	learner, err := OpenAuthorityLearner(path, current.Grant, exploration)
	if err != nil {
		return nil, err
	}
	return &ChainedAuthorityLearner{journalPath: path, chain: cloneAuthorityChain(chain), learner: learner}, nil
}

func (l *ChainedAuthorityLearner) Chain() AuthorityChain { return cloneAuthorityChain(l.chain) }
func (l *ChainedAuthorityLearner) CurrentSignedGrant() SignedAuthorityGrant {
	return cloneSignedAuthority(l.chain.Grants[len(l.chain.Grants)-1])
}
func (l *ChainedAuthorityLearner) Choose(ctx Context) (ContextDecision, error) {
	return l.learner.Choose(ctx)
}
func (l *ChainedAuthorityLearner) SnapshotContext(ctx Context) (map[string]ActionStat, error) {
	return l.learner.SnapshotContext(ctx)
}
func (l *ChainedAuthorityLearner) BestObserved(ctx Context) (string, ActionStat, bool, error) {
	return l.learner.BestObserved(ctx)
}
func (l *ChainedAuthorityLearner) JournalHead() (uint64, string) { return l.learner.JournalHead() }

func (l *ChainedAuthorityLearner) Apply(receipt metro.Receipt, result map[string]any, exp Experience) (ApplyResult, error) {
	current := l.CurrentSignedGrant()
	if err := validateSignedAuthorityEvidence(result, exp, current); err != nil {
		return ApplyResult{}, err
	}
	return l.learner.Apply(receipt, result, exp)
}

// Rotate creates a rotation anchored to the actual current journal head, appends
// it to the authority chain, and opens a fresh journal for the next epoch.
// Learned policy state is intentionally not migrated across authority epochs.
func (l *ChainedAuthorityLearner) Rotate(next SignedAuthorityGrant, nextJournalPath string, currentPrivateKey ed25519.PrivateKey, exploration float64) (*ChainedAuthorityLearner, AuthorityRotation, error) {
	if nextJournalPath == "" || nextJournalPath == l.journalPath {
		return nil, AuthorityRotation{}, errors.New("authority rotation requires a distinct next journal path")
	}
	if err := l.chain.Verify(); err != nil {
		return nil, AuthorityRotation{}, err
	}
	current := l.CurrentSignedGrant()
	sequence, head := l.JournalHead()
	rotation, err := SignAuthorityRotation(current, next, sequence, head, currentPrivateKey)
	if err != nil {
		return nil, AuthorityRotation{}, err
	}
	newChain := cloneAuthorityChain(l.chain)
	newChain.Grants = append(newChain.Grants, cloneSignedAuthority(next))
	newChain.Rotations = append(newChain.Rotations, rotation)
	if err := newChain.Verify(); err != nil {
		return nil, AuthorityRotation{}, err
	}
	nextLearner, err := OpenChainedAuthorityLearner(nextJournalPath, newChain, exploration)
	if err != nil {
		return nil, AuthorityRotation{}, err
	}
	return nextLearner, rotation, nil
}

func validateSignedAuthorityJournal(path string, signed SignedAuthorityGrant) error {
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
			return fmt.Errorf("signed authority journal line %d is not valid JSON: %w", line, err)
		}
		if err := validateSignedAuthorityEvidence(entry.Result, entry.Experience, signed); err != nil {
			return fmt.Errorf("signed authority journal line %d rejected: %w", line, err)
		}
	}
	return scanner.Err()
}

func cloneSignedAuthority(src SignedAuthorityGrant) SignedAuthorityGrant {
	dst := src
	dst.Grant.AllowedActions = append([]string(nil), src.Grant.AllowedActions...)
	return dst
}

func cloneAuthorityChain(src AuthorityChain) AuthorityChain {
	dst := src
	dst.Grants = make([]SignedAuthorityGrant, len(src.Grants))
	for i := range src.Grants {
		dst.Grants[i] = cloneSignedAuthority(src.Grants[i])
	}
	dst.Rotations = append([]AuthorityRotation(nil), src.Rotations...)
	return dst
}

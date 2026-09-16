package deviceattest

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	EvidenceProtocol    = "liminal.device.evidence.v0.1"
	AttestationProtocol = "liminal.device.attestation.v0.1"
	TrustAnchorProtocol = "liminal.device.trust-anchor.v0.1"
	RootTypeSoftware    = "software-observed"
)

type Evidence struct {
	Protocol           string            `json:"protocol"`
	RootType           string            `json:"root_type"`
	OS                 string            `json:"os"`
	Arch               string            `json:"arch"`
	KernelRelease      string            `json:"kernel_release,omitempty"`
	GoVersion          string            `json:"go_version"`
	LogicalCPUs        int               `json:"logical_cpus"`
	SourceDigests      map[string]string `json:"source_digests"`
	DeviceFingerprint  string            `json:"device_fingerprint"`
	RuntimeFingerprint string            `json:"runtime_fingerprint"`
	ObservedAt         string            `json:"observed_at"`
}

type fingerprintMaterial struct {
	Protocol      string            `json:"protocol"`
	RootType      string            `json:"root_type"`
	OS            string            `json:"os"`
	Arch          string            `json:"arch"`
	SourceDigests map[string]string `json:"source_digests"`
}

type runtimeMaterial struct {
	DeviceFingerprint string `json:"device_fingerprint"`
	KernelRelease     string `json:"kernel_release"`
	GoVersion         string `json:"go_version"`
	LogicalCPUs       int    `json:"logical_cpus"`
	BootDigest        string `json:"boot_digest,omitempty"`
}

func Sense() (Evidence, error) {
	sources := map[string]string{}
	addDigest(sources, "machine_id", "/etc/machine-id")
	addDigest(sources, "dmi_product_uuid", "/sys/class/dmi/id/product_uuid")
	addDigest(sources, "boot_id", "/proc/sys/kernel/random/boot_id")
	if host, err := os.Hostname(); err == nil && strings.TrimSpace(host) != "" {
		sources["hostname"] = hashBytes([]byte(strings.TrimSpace(host)))
	}
	kernel := kernelRelease()
	stableSources := map[string]string{}
	for _, key := range []string{"machine_id", "dmi_product_uuid", "hostname"} {
		if v := sources[key]; v != "" {
			stableSources[key] = v
		}
	}
	if len(stableSources) == 0 {
		return Evidence{}, errors.New("no stable software-observed device sources available")
	}
	dev, err := hashJSON(fingerprintMaterial{Protocol: EvidenceProtocol, RootType: RootTypeSoftware, OS: runtime.GOOS, Arch: runtime.GOARCH, SourceDigests: stableSources})
	if err != nil {
		return Evidence{}, err
	}
	run, err := hashJSON(runtimeMaterial{DeviceFingerprint: dev, KernelRelease: kernel, GoVersion: runtime.Version(), LogicalCPUs: runtime.NumCPU(), BootDigest: sources["boot_id"]})
	if err != nil {
		return Evidence{}, err
	}
	return Evidence{Protocol: EvidenceProtocol, RootType: RootTypeSoftware, OS: runtime.GOOS, Arch: runtime.GOARCH, KernelRelease: kernel, GoVersion: runtime.Version(), LogicalCPUs: runtime.NumCPU(), SourceDigests: sources, DeviceFingerprint: dev, RuntimeFingerprint: run, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}, nil
}

func (e Evidence) Validate() error {
	if e.Protocol != EvidenceProtocol || e.RootType != RootTypeSoftware {
		return errors.New("unsupported device evidence protocol/root type")
	}
	if e.OS == "" || e.Arch == "" || e.GoVersion == "" || e.LogicalCPUs < 1 {
		return errors.New("device evidence runtime fields are incomplete")
	}
	if !shaHex(e.DeviceFingerprint) || !shaHex(e.RuntimeFingerprint) {
		return errors.New("device/runtime fingerprints must be sha256 hex")
	}
	stable := map[string]string{}
	for _, key := range []string{"machine_id", "dmi_product_uuid", "hostname"} {
		if v := e.SourceDigests[key]; v != "" {
			stable[key] = v
		}
	}
	expectedDevice, err := hashJSON(fingerprintMaterial{Protocol: e.Protocol, RootType: e.RootType, OS: e.OS, Arch: e.Arch, SourceDigests: stable})
	if err != nil {
		return err
	}
	if expectedDevice != e.DeviceFingerprint {
		return errors.New("device fingerprint does not match evidence")
	}
	expectedRuntime, err := hashJSON(runtimeMaterial{DeviceFingerprint: e.DeviceFingerprint, KernelRelease: e.KernelRelease, GoVersion: e.GoVersion, LogicalCPUs: e.LogicalCPUs, BootDigest: e.SourceDigests["boot_id"]})
	if err != nil {
		return err
	}
	if expectedRuntime != e.RuntimeFingerprint {
		return errors.New("runtime fingerprint does not match evidence")
	}
	return nil
}

type TrustAnchor struct {
	Protocol          string `json:"protocol"`
	RootType          string `json:"root_type"`
	DeviceFingerprint string `json:"device_fingerprint"`
	AttestationKeyID  string `json:"attestation_key_id"`
}

func NewTrustAnchor(e Evidence, publicKey ed25519.PublicKey) (TrustAnchor, error) {
	if err := e.Validate(); err != nil {
		return TrustAnchor{}, err
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return TrustAnchor{}, errors.New("invalid device attestation public key")
	}
	return TrustAnchor{Protocol: TrustAnchorProtocol, RootType: RootTypeSoftware, DeviceFingerprint: e.DeviceFingerprint, AttestationKeyID: KeyID(publicKey)}, nil
}

func GenerateKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}
func KeyID(publicKey ed25519.PublicKey) string { return hashBytes(publicKey) }
func RandomNonce() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

type Attestation struct {
	Protocol             string `json:"protocol"`
	RootType             string `json:"root_type"`
	Nonce                string `json:"nonce"`
	DeviceFingerprint    string `json:"device_fingerprint"`
	RuntimeFingerprint   string `json:"runtime_fingerprint"`
	AttestationKeyID     string `json:"attestation_key_id"`
	PublicKeyBase64      string `json:"public_key_base64"`
	SignedAuthorityHash  string `json:"signed_authority_hash"`
	AuthorityIssuerKeyID string `json:"authority_issuer_key_id"`
	JournalSequence      uint64 `json:"journal_sequence"`
	JournalHead          string `json:"journal_head,omitempty"`
	Workload             string `json:"workload"`
	ContextKey           string `json:"context_key"`
	IssuedAt             string `json:"issued_at"`
	SignatureBase64      string `json:"signature_base64"`
	AttestationHash      string `json:"attestation_hash"`
}

type attestationMaterial struct {
	Protocol             string `json:"protocol"`
	RootType             string `json:"root_type"`
	Nonce                string `json:"nonce"`
	DeviceFingerprint    string `json:"device_fingerprint"`
	RuntimeFingerprint   string `json:"runtime_fingerprint"`
	AttestationKeyID     string `json:"attestation_key_id"`
	PublicKeyBase64      string `json:"public_key_base64"`
	SignedAuthorityHash  string `json:"signed_authority_hash"`
	AuthorityIssuerKeyID string `json:"authority_issuer_key_id"`
	JournalSequence      uint64 `json:"journal_sequence"`
	JournalHead          string `json:"journal_head,omitempty"`
	Workload             string `json:"workload"`
	ContextKey           string `json:"context_key"`
	IssuedAt             string `json:"issued_at"`
}

func (a Attestation) material() attestationMaterial {
	return attestationMaterial{a.Protocol, a.RootType, a.Nonce, a.DeviceFingerprint, a.RuntimeFingerprint, a.AttestationKeyID, a.PublicKeyBase64, a.SignedAuthorityHash, a.AuthorityIssuerKeyID, a.JournalSequence, a.JournalHead, a.Workload, a.ContextKey, a.IssuedAt}
}

func Sign(e Evidence, privateKey ed25519.PrivateKey, nonce, signedAuthorityHash, issuerKeyID string, seq uint64, head, workload, contextKey string) (Attestation, error) {
	if err := e.Validate(); err != nil {
		return Attestation{}, err
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return Attestation{}, errors.New("invalid device private key")
	}
	if nonce == "" || workload == "" || contextKey == "" || !shaHex(signedAuthorityHash) || !shaHex(issuerKeyID) {
		return Attestation{}, errors.New("attestation binding fields are incomplete")
	}
	if seq > 0 && !shaHex(head) {
		return Attestation{}, errors.New("non-empty journal requires sha256 head")
	}
	if seq == 0 && head != "" {
		return Attestation{}, errors.New("empty journal requires empty head")
	}
	pub := privateKey.Public().(ed25519.PublicKey)
	a := Attestation{Protocol: AttestationProtocol, RootType: RootTypeSoftware, Nonce: nonce, DeviceFingerprint: e.DeviceFingerprint, RuntimeFingerprint: e.RuntimeFingerprint, AttestationKeyID: KeyID(pub), PublicKeyBase64: base64.StdEncoding.EncodeToString(pub), SignedAuthorityHash: signedAuthorityHash, AuthorityIssuerKeyID: issuerKeyID, JournalSequence: seq, JournalHead: head, Workload: workload, ContextKey: contextKey, IssuedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	b, err := json.Marshal(a.material())
	if err != nil {
		return Attestation{}, err
	}
	a.SignatureBase64 = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, b))
	a.AttestationHash, err = hashJSON(struct {
		Material  attestationMaterial `json:"material"`
		Signature string              `json:"signature_base64"`
	}{a.material(), a.SignatureBase64})
	return a, err
}

func (a Attestation) SelfVerify() error {
	if a.Protocol != AttestationProtocol || a.RootType != RootTypeSoftware {
		return errors.New("unsupported attestation protocol/root type")
	}
	if a.Nonce == "" || a.Workload == "" || a.ContextKey == "" || !shaHex(a.DeviceFingerprint) || !shaHex(a.RuntimeFingerprint) || !shaHex(a.AttestationKeyID) || !shaHex(a.SignedAuthorityHash) || !shaHex(a.AuthorityIssuerKeyID) || !shaHex(a.AttestationHash) {
		return errors.New("attestation fields are incomplete")
	}
	if a.JournalSequence > 0 && !shaHex(a.JournalHead) {
		return errors.New("attestation journal head invalid")
	}
	pubBytes, err := base64.StdEncoding.DecodeString(a.PublicKeyBase64)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return errors.New("invalid attestation public key")
	}
	pub := ed25519.PublicKey(pubBytes)
	if KeyID(pub) != a.AttestationKeyID {
		return errors.New("attestation key id mismatch")
	}
	sig, err := base64.StdEncoding.DecodeString(a.SignatureBase64)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("invalid attestation signature")
	}
	b, err := json.Marshal(a.material())
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, b, sig) {
		return errors.New("attestation signature verification failed")
	}
	expected, err := hashJSON(struct {
		Material  attestationMaterial `json:"material"`
		Signature string              `json:"signature_base64"`
	}{a.material(), a.SignatureBase64})
	if err != nil {
		return err
	}
	if expected != a.AttestationHash {
		return errors.New("attestation hash mismatch")
	}
	return nil
}

func VerifyBound(a Attestation, anchor TrustAnchor, expectedNonce string, current Evidence, signedAuthorityHash, issuerKeyID string, seq uint64, head, workload, contextKey string) error {
	if err := a.SelfVerify(); err != nil {
		return err
	}
	if err := current.Validate(); err != nil {
		return err
	}
	if anchor.Protocol != TrustAnchorProtocol || anchor.RootType != RootTypeSoftware || !shaHex(anchor.DeviceFingerprint) || !shaHex(anchor.AttestationKeyID) {
		return errors.New("invalid device trust anchor")
	}
	if a.DeviceFingerprint != anchor.DeviceFingerprint || a.AttestationKeyID != anchor.AttestationKeyID {
		return errors.New("attestation is not rooted in pinned software device identity")
	}
	if a.DeviceFingerprint != current.DeviceFingerprint || a.RuntimeFingerprint != current.RuntimeFingerprint {
		return errors.New("attestation does not match current runtime evidence")
	}
	if a.Nonce != expectedNonce {
		return errors.New("attestation nonce mismatch/replay")
	}
	if a.SignedAuthorityHash != signedAuthorityHash || a.AuthorityIssuerKeyID != issuerKeyID {
		return errors.New("attestation authority binding mismatch")
	}
	if a.JournalSequence != seq || a.JournalHead != head {
		return errors.New("attestation journal anchor mismatch")
	}
	if a.Workload != workload || a.ContextKey != contextKey {
		return errors.New("attestation workload/context mismatch")
	}
	return nil
}

func addDigest(dst map[string]string, key, path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	v := strings.TrimSpace(string(b))
	if v != "" {
		dst[key] = hashBytes([]byte(v))
	}
}
func kernelRelease() string {
	out, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
func hashBytes(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func hashJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return hashBytes(b), nil
}
func shaHex(v string) bool {
	if len(v) != 64 || v != strings.ToLower(v) {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil
}

func SortedSourceKeys(e Evidence) []string {
	keys := make([]string, 0, len(e.SourceDigests))
	for k := range e.SourceDigests {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func Summary(e Evidence) string {
	return fmt.Sprintf("root=%s os=%s arch=%s cpus=%d device=%s runtime=%s", e.RootType, e.OS, e.Arch, e.LogicalCPUs, e.DeviceFingerprint[:12], e.RuntimeFingerprint[:12])
}

package lifetrabridge

import (
	"errors"

	"github.com/safal207/Liminal-Rail-Metro/internal/metro"
)

const (
	DeviceAttestationRefPrefix  = "adaptive-device-attestation://sha256/"
	DeviceKeyRefPrefix          = "adaptive-device-key://ed25519/sha256/"
	DeviceFingerprintRefPrefix  = "adaptive-device-fingerprint://sha256/"
	RuntimeFingerprintRefPrefix = "adaptive-runtime-fingerprint://sha256/"
)

// ReceiptToDeviceAttestedObservation extends the v0.5 signed-authority proof
// chain with software-observed device identity and per-attempt attestation refs.
// These refs do not claim TPM/TEE or remote-attestation provenance.
func ReceiptToDeviceAttestedObservation(receipt metro.Receipt, previousBeadRef, journalEntryHash, authorityHash, signedAuthorityHash, issuerKeyID, rotationHash, attestationHash, deviceKeyID, deviceFingerprint, runtimeFingerprint string) (Observation, error) {
	for _, value := range []string{attestationHash, deviceKeyID, deviceFingerprint, runtimeFingerprint} {
		if !sha256Hex(value) {
			return Observation{}, errors.New("device-attested observation requires sha256 attestation/key/device/runtime identifiers")
		}
	}
	obs, err := ReceiptToSignedAuthorityObservation(receipt, previousBeadRef, journalEntryHash, authorityHash, signedAuthorityHash, issuerKeyID, rotationHash)
	if err != nil {
		return Observation{}, err
	}
	obs.ProofRefs = appendUnique(obs.ProofRefs, DeviceAttestationRefPrefix+attestationHash)
	obs.ProofRefs = appendUnique(obs.ProofRefs, DeviceKeyRefPrefix+deviceKeyID)
	obs.ProofRefs = appendUnique(obs.ProofRefs, DeviceFingerprintRefPrefix+deviceFingerprint)
	obs.ProofRefs = appendUnique(obs.ProofRefs, RuntimeFingerprintRefPrefix+runtimeFingerprint)
	return obs, nil
}

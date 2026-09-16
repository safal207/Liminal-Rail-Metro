package adaptive

import (
	"errors"

	"github.com/safal207/Liminal-Rail-Metro/internal/deviceattest"
)

func BindDiscoveryResult(result map[string]any, report deviceattest.DiscoveryReport) (map[string]any, error) {
	if err := report.Validate(); err != nil {
		return nil, err
	}
	bound := cloneResult(result)
	bound["attestation_discovery_protocol"] = report.Protocol
	bound["attestation_discovery_hash"] = report.ReportHash
	bound["attestation_selected_provider_id"] = report.SelectedProviderID
	bound["attestation_selected_assurance_level"] = report.SelectedAssuranceLevel
	bound["attestation_hardware_upgrade_selected"] = report.HardwareUpgradeSelected
	bound["attestation_external_identity_verified"] = report.ExternalIdentityVerified
	return bound, nil
}

func ValidateDiscoveryResultBinding(result map[string]any, report deviceattest.DiscoveryReport) error {
	if err := report.Validate(); err != nil {
		return err
	}
	if valueString(result, "attestation_discovery_protocol") != report.Protocol ||
		valueString(result, "attestation_discovery_hash") != report.ReportHash ||
		valueString(result, "attestation_selected_provider_id") != report.SelectedProviderID ||
		valueString(result, "attestation_selected_assurance_level") != report.SelectedAssuranceLevel {
		return errors.New("measured result is not bound to attestation discovery report")
	}
	if got, ok := result["attestation_hardware_upgrade_selected"].(bool); !ok || got != report.HardwareUpgradeSelected {
		return errors.New("measured result hardware-upgrade discovery flag mismatch")
	}
	if got, ok := result["attestation_external_identity_verified"].(bool); !ok || got != report.ExternalIdentityVerified {
		return errors.New("measured result external-identity discovery flag mismatch")
	}
	return nil
}

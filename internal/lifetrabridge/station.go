package lifetrabridge

import (
	"errors"
	"fmt"
	"math"
)

const StationRequestProtocol = "lifetra.station.request.v0.1"

type Orientation struct {
	Growth     float64 `json:"growth"`
	Stability  float64 `json:"stability"`
	Truth      float64 `json:"truth"`
	Connection float64 `json:"connection"`
}

type CorrectionConfig struct {
	Gain             float64 `json:"gain"`
	EngageThreshold  float64 `json:"engage_threshold"`
	ReleaseThreshold float64 `json:"release_threshold"`
	MaxStep          float64 `json:"max_step"`
}

type SafetyConfig struct {
	Autonomy                 string  `json:"autonomy"`
	MinProofRefs             int     `json:"min_proof_refs"`
	QuorumRequired           int     `json:"quorum_required"`
	RequireHumanApproval     bool    `json:"require_human_approval"`
	MaxAutonomousAdjustment  float64 `json:"max_autonomous_adjustment"`
	HardMaxAdjustment        float64 `json:"hard_max_adjustment"`
}

type AuthorityContext struct {
	HumanApproval  string `json:"human_approval"`
	QuorumApprovals int   `json:"quorum_approvals"`
}

type StationRequest struct {
	Protocol             string           `json:"protocol"`
	Observation          Observation      `json:"observation"`
	ObservedEpochSeconds int64            `json:"observed_epoch_seconds"`
	DecidedAt            string           `json:"decided_at"`
	IntendedOrientation  Orientation      `json:"intended_orientation"`
	ObservedOrientation  *Orientation     `json:"observed_orientation,omitempty"`
	Correction           CorrectionConfig `json:"correction"`
	Safety               SafetyConfig     `json:"safety"`
	AuthorityContext     AuthorityContext `json:"authority_context"`
	NextAction           NextAction       `json:"next_action"`
}

func (request StationRequest) Validate() error {
	if request.Protocol != StationRequestProtocol {
		return fmt.Errorf("unsupported station request protocol %q", request.Protocol)
	}
	if request.Observation.Protocol != ObservationProtocol {
		return fmt.Errorf("unsupported observation protocol %q", request.Observation.Protocol)
	}
	if request.Observation.ObservationID == "" || request.Observation.ActionID == "" || request.Observation.ReceiptID == "" {
		return errors.New("observation_id, action_id, and receipt_id are required")
	}
	if request.ObservedEpochSeconds <= 0 || request.DecidedAt == "" {
		return errors.New("observed_epoch_seconds and decided_at are required")
	}
	if err := validateOrientation(request.IntendedOrientation, "intended_orientation"); err != nil {
		return err
	}
	if request.ObservedOrientation != nil {
		if err := validateOrientation(*request.ObservedOrientation, "observed_orientation"); err != nil {
			return err
		}
	}
	if request.Observation.ReceiptStatus == "SUCCEEDED" || request.Observation.ReceiptStatus == "FAILED" {
		if request.ObservedOrientation == nil {
			return errors.New("observed_orientation is required for confirmed outcomes")
		}
	}
	if request.Correction.ReleaseThreshold > request.Correction.EngageThreshold {
		return errors.New("release_threshold must not exceed engage_threshold")
	}
	if !normalized(request.Correction.Gain, request.Correction.EngageThreshold, request.Correction.ReleaseThreshold, request.Correction.MaxStep) {
		return errors.New("correction values must be finite and normalized to 0..=1")
	}
	if request.Safety.MinProofRefs < 0 || request.Safety.QuorumRequired < 0 {
		return errors.New("safety counts cannot be negative")
	}
	if !normalized(request.Safety.MaxAutonomousAdjustment, request.Safety.HardMaxAdjustment) {
		return errors.New("safety adjustment bounds must be finite and normalized to 0..=1")
	}
	if request.Safety.MaxAutonomousAdjustment > request.Safety.HardMaxAdjustment {
		return errors.New("max_autonomous_adjustment must not exceed hard_max_adjustment")
	}
	if request.NextAction.ActionID == "" || request.NextAction.Goal == "" || request.NextAction.Kind == "" {
		return errors.New("next_action action_id, goal, and kind are required")
	}
	if request.NextAction.ActionID == request.Observation.ActionID {
		return errors.New("next_action must not silently reuse the prior action_id")
	}
	if len(request.NextAction.AllowedTargets) == 0 || !uniqueNonEmpty(request.NextAction.AllowedTargets) {
		return errors.New("next_action allowed_targets must be non-empty and unique")
	}
	if request.NextAction.TimeoutMS < 0 {
		return errors.New("timeout_ms cannot be negative")
	}
	return nil
}

func validateOrientation(value Orientation, name string) error {
	if !normalized(value.Growth, value.Stability, value.Truth, value.Connection) {
		return fmt.Errorf("%s values must be finite and normalized to 0..=1", name)
	}
	return nil
}

func normalized(values ...float64) bool {
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return false
		}
	}
	return true
}

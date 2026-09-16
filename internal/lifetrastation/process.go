package lifetrastation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/lifetrabridge"
)

type Process struct {
	Command string
	Args    []string
	Dir     string
	Env     []string
	Timeout time.Duration
}

func (station Process) Evaluate(ctx context.Context, request lifetrabridge.StationRequest) (lifetrabridge.Decision, error) {
	if err := request.Validate(); err != nil {
		return lifetrabridge.Decision{}, fmt.Errorf("validate station request: %w", err)
	}
	if station.Command == "" {
		return lifetrabridge.Decision{}, errors.New("station command is required")
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return lifetrabridge.Decision{}, fmt.Errorf("encode station request: %w", err)
	}

	if station.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, station.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, station.Command, station.Args...)
	cmd.Dir = station.Dir
	cmd.Stdin = bytes.NewReader(payload)
	if len(station.Env) > 0 {
		cmd.Env = append(os.Environ(), station.Env...)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return lifetrabridge.Decision{}, fmt.Errorf("station process: %w", ctx.Err())
		}
		return lifetrabridge.Decision{}, fmt.Errorf("station process failed: %w: %s", err, stderr.String())
	}

	decoder := json.NewDecoder(&stdout)
	decoder.DisallowUnknownFields()
	var decision lifetrabridge.Decision
	if err := decoder.Decode(&decision); err != nil {
		return lifetrabridge.Decision{}, fmt.Errorf("decode station decision: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return lifetrabridge.Decision{}, err
	}
	if err := validateBinding(request, decision); err != nil {
		return lifetrabridge.Decision{}, err
	}
	return decision, nil
}

func validateBinding(request lifetrabridge.StationRequest, decision lifetrabridge.Decision) error {
	if decision.Protocol != lifetrabridge.DecisionProtocol {
		return fmt.Errorf("unexpected decision protocol %q", decision.Protocol)
	}
	if decision.SourceObservationID != request.Observation.ObservationID {
		return errors.New("decision source_observation_id does not match request")
	}
	if decision.CausedByActionID != request.Observation.ActionID {
		return errors.New("decision caused_by_action_id does not match request")
	}
	expectedReceiptRef := "metro-receipt://" + request.Observation.ReceiptID
	if decision.SourceReceiptRef != expectedReceiptRef {
		return errors.New("decision source_receipt_ref does not match request")
	}

	switch decision.Verdict {
	case lifetrabridge.VerdictAllow:
		if _, err := lifetrabridge.DecisionToPacket(decision); err != nil {
			return fmt.Errorf("ALLOW decision is not dispatchable: %w", err)
		}
	case lifetrabridge.VerdictRequireApproval, lifetrabridge.VerdictBlock:
		if decision.AuthorityProofRef != "" || decision.NextAction != nil {
			return errors.New("non-ALLOW decision must not contain dispatch authority or next_action")
		}
	default:
		return fmt.Errorf("unsupported decision verdict %q", decision.Verdict)
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing station output: %w", err)
	}
	return errors.New("station process emitted more than one JSON value")
}

package lifetrastation

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/lifetrabridge"
)

func TestMultiplexCorrelatesOutOfOrderResponses(t *testing.T) {
	station := &MultiplexProcess{Config: ProcessConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestMuxHelperProcess"},
		Env: []string{
			"GO_WANT_MUX_HELPER=1",
			"MUX_HELPER_MODE=reverse-two",
		},
		Timeout: 5 * time.Second,
	}}
	if err := station.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := station.Close(); err != nil {
			t.Errorf("close station: %v", err)
		}
	}()

	type result struct {
		requestID string
		actionID  string
		err       error
	}
	results := make(chan result, 2)

	for _, item := range []struct {
		requestID string
		actionID  string
		nextID    string
	}{
		{requestID: "req-a", actionID: "action-a", nextID: "next-a"},
		{requestID: "req-b", actionID: "action-b", nextID: "next-b"},
	} {
		item := item
		go func() {
			decision, err := station.Evaluate(context.Background(), item.requestID, muxTestRequest(item.actionID, item.nextID))
			results <- result{requestID: item.requestID, actionID: decision.CausedByActionID, err: err}
		}()
	}

	seen := map[string]string{}
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		seen[result.requestID] = result.actionID
	}
	if seen["req-a"] != "action-a" || seen["req-b"] != "action-b" {
		t.Fatalf("out-of-order responses were miscorrelated: %#v", seen)
	}
}

func TestMultiplexRejectsRequestIDReuse(t *testing.T) {
	station := &MultiplexProcess{Config: muxHelperConfig("normal")}
	if err := station.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = station.Close() }()

	if _, err := station.Evaluate(context.Background(), "req-once", muxTestRequest("action-1", "next-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := station.Evaluate(context.Background(), "req-once", muxTestRequest("action-2", "next-2")); err == nil {
		t.Fatal("expected request_id reuse to fail closed")
	}
}

func TestMultiplexBindingMismatchFailsOnlyBoundRequest(t *testing.T) {
	station := &MultiplexProcess{Config: muxHelperConfig("mismatch")}
	if err := station.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = station.Close() }()

	if _, err := station.Evaluate(context.Background(), "req-mismatch", muxTestRequest("action-1", "next-1")); err == nil {
		t.Fatal("expected mismatched decision binding to fail closed")
	}
}

func TestPoolRoutesConcurrentRequestsAcrossPersistentProcesses(t *testing.T) {
	pool, err := StartPool(muxHelperConfig("normal"), 2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := pool.Close(); err != nil {
			t.Errorf("close pool: %v", err)
		}
	}()

	pids := pool.PIDs()
	if len(pids) != 2 || pids[0] == 0 || pids[1] == 0 || pids[0] == pids[1] {
		t.Fatalf("expected two distinct persistent PIDs, got %#v", pids)
	}

	const requests = 16
	var wg sync.WaitGroup
	errs := make(chan error, requests)
	for index := 0; index < requests; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			actionID := fmt.Sprintf("pool-action-%02d", index)
			decision, err := pool.Evaluate(
				context.Background(),
				fmt.Sprintf("pool-request-%02d", index),
				muxTestRequest(actionID, fmt.Sprintf("pool-next-%02d", index)),
			)
			if err != nil {
				errs <- err
				return
			}
			if decision.CausedByActionID != actionID {
				errs <- fmt.Errorf("request %d returned action %q", index, decision.CausedByActionID)
			}
		}(index)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func muxHelperConfig(mode string) ProcessConfig {
	return muxHelperConfigWithDelay(mode, 0)
}

func muxHelperConfigWithDelay(mode string, delay time.Duration) ProcessConfig {
	return ProcessConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestMuxHelperProcess"},
		Env: []string{
			"GO_WANT_MUX_HELPER=1",
			"MUX_HELPER_MODE=" + mode,
			"MUX_HELPER_DELAY=" + delay.String(),
		},
		Timeout: 5 * time.Second,
	}
}

func muxTestRequest(actionID, nextActionID string) lifetrabridge.StationRequest {
	request := testRequest()
	request.Observation.ObservationID = "observation-" + actionID
	request.Observation.ActionID = actionID
	request.Observation.ReceiptID = "receipt-" + actionID
	request.Observation.ProofRefs = []string{"metro-receipt://receipt-" + actionID}
	request.NextAction.ActionID = nextActionID
	return request
}

func TestMuxHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_MUX_HELPER") != "1" {
		return
	}

	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	mode := os.Getenv("MUX_HELPER_MODE")
	delay := time.Duration(0)
	if raw := os.Getenv("MUX_HELPER_DELAY"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			os.Exit(5)
		}
		delay = parsed
	}

	var buffered []RequestEnvelope
	for scanner.Scan() {
		var envelope RequestEnvelope
		if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
			os.Exit(2)
		}
		if mode == "reverse-two" {
			buffered = append(buffered, envelope)
			if len(buffered) < 2 {
				continue
			}
			for index := len(buffered) - 1; index >= 0; index-- {
				if delay > 0 {
					time.Sleep(delay)
				}
				if err := encoder.Encode(helperResponse(buffered[index], "normal")); err != nil {
					os.Exit(3)
				}
			}
			buffered = nil
			continue
		}

		if delay > 0 {
			time.Sleep(delay)
		}
		if err := encoder.Encode(helperResponse(envelope, mode)); err != nil {
			os.Exit(3)
		}
	}
	if err := scanner.Err(); err != nil {
		os.Exit(4)
	}
	os.Exit(0)
}

func helperResponse(envelope RequestEnvelope, mode string) ResponseEnvelope {
	observationID := envelope.Request.Observation.ObservationID
	if mode == "mismatch" {
		observationID = "observation-wrong"
	}
	decision := lifetrabridge.Decision{
		Protocol:            lifetrabridge.DecisionProtocol,
		DecisionID:          "decision-" + envelope.RequestID,
		SourceObservationID: observationID,
		SourceReceiptRef:    "metro-receipt://" + envelope.Request.Observation.ReceiptID,
		CausedByActionID:    envelope.Request.Observation.ActionID,
		SourceBeadRef:       "lifetra-bead://" + envelope.RequestID,
		Verdict:             lifetrabridge.VerdictAllow,
		AuthorityProofRef:   "lifetra-authority://" + envelope.RequestID,
		NextAction:          &envelope.Request.NextAction,
		DecidedAt:           envelope.Request.DecidedAt,
	}
	return ResponseEnvelope{
		Protocol:  ResponseEnvelopeProtocol,
		RequestID: envelope.RequestID,
		Decision:  decision,
	}
}

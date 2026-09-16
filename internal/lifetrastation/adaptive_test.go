package lifetrastation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestAdaptiveRouterPrefersLowerEstimatedDelay(t *testing.T) {
	config, err := (AdaptivePoolConfig{
		MaxInFlightPerStation: 4,
		InitialLatency:        time.Millisecond,
		EWMAAlpha:             0.2,
	}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	pool := newAdaptivePoolFromStations(
		[]*MultiplexProcess{{}, {}},
		config,
	)
	pool.stations[0].completed = 1
	pool.stations[0].ewma = 4 * time.Millisecond
	pool.stations[1].completed = 1
	pool.stations[1].ewma = 500 * time.Microsecond
	pool.stations[1].inFlight = 1

	pool.lock()
	index := pool.chooseStationLocked()
	pool.unlock()

	if index != 1 {
		t.Fatalf("expected lower estimated delay station 1, got %d", index)
	}
}

func TestAdaptivePoolBackpressureDoesNotConsumeRequestID(t *testing.T) {
	config, err := (AdaptivePoolConfig{
		MaxInFlightPerStation: 1,
		InitialLatency:        200 * time.Microsecond,
		EWMAAlpha:             0.2,
	}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	pool := newAdaptivePoolFromStations(
		[]*MultiplexProcess{{}, {}},
		config,
	)
	pool.stations[0].inFlight = 1
	pool.stations[1].inFlight = 1

	_, err = pool.reserve("retryable-request")
	if !errors.Is(err, ErrBackpressure) {
		t.Fatalf("expected typed backpressure, got %v", err)
	}
	var pressure *BackpressureError
	if !errors.As(err, &pressure) {
		t.Fatalf("expected BackpressureError, got %T", err)
	}
	if pressure.InFlight != 2 || pressure.Capacity != 2 {
		t.Fatalf("unexpected pressure state: %#v", pressure)
	}

	pool.stations[1].inFlight = 0
	state, err := pool.reserve("retryable-request")
	if err != nil {
		t.Fatalf("request id should remain reusable after non-dispatch backpressure: %v", err)
	}
	pool.finish(state, 100*time.Microsecond, nil)
}

func TestAdaptivePoolCapacityIsNeverOversubscribed(t *testing.T) {
	config, err := (AdaptivePoolConfig{
		MaxInFlightPerStation: 3,
		InitialLatency:        time.Millisecond,
		EWMAAlpha:             0.25,
	}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	pool := newAdaptivePoolFromStations(
		[]*MultiplexProcess{{}, {}},
		config,
	)

	reserved := make([]*adaptiveStation, 0, 6)
	for index := 0; index < 6; index++ {
		state, err := pool.reserve(fmt.Sprintf("capacity-%d", index))
		if err != nil {
			t.Fatalf("reserve %d: %v", index, err)
		}
		reserved = append(reserved, state)
	}
	if _, err := pool.reserve("capacity-overflow"); !errors.Is(err, ErrBackpressure) {
		t.Fatalf("expected backpressure at exact pool capacity, got %v", err)
	}

	for index, snapshot := range pool.Snapshots() {
		if snapshot.InFlight > snapshot.MaxInFlight {
			t.Fatalf("station %d oversubscribed: %#v", index, snapshot)
		}
		if snapshot.InFlight != 3 {
			t.Fatalf("expected balanced 3/3 admission, got station %d = %d", index, snapshot.InFlight)
		}
	}

	for _, state := range reserved {
		pool.finish(state, time.Millisecond, nil)
	}
}

func TestAdaptivePoolExploresStationsAndTracksEWMA(t *testing.T) {
	pool, err := StartAdaptivePool(
		muxHelperConfig("normal"),
		2,
		AdaptivePoolConfig{
			MaxInFlightPerStation: 2,
			InitialLatency:        time.Millisecond,
			EWMAAlpha:             0.5,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := pool.Close(); err != nil {
			t.Errorf("close adaptive pool: %v", err)
		}
	}()

	for index := 0; index < 2; index++ {
		actionID := fmt.Sprintf("adaptive-action-%d", index)
		decision, err := pool.Evaluate(
			context.Background(),
			fmt.Sprintf("adaptive-request-%d", index),
			muxTestRequest(actionID, fmt.Sprintf("adaptive-next-%d", index)),
		)
		if err != nil {
			t.Fatal(err)
		}
		if decision.CausedByActionID != actionID {
			t.Fatalf("request %d rebound to %q", index, decision.CausedByActionID)
		}
	}

	snapshots := pool.Snapshots()
	if len(snapshots) != 2 {
		t.Fatalf("expected two snapshots, got %d", len(snapshots))
	}
	for index, snapshot := range snapshots {
		if snapshot.Completed != 1 {
			t.Fatalf("expected cold exploration to sample station %d exactly once, got %#v", index, snapshot)
		}
		if snapshot.EWMALatency <= 0 {
			t.Fatalf("station %d did not record latency: %#v", index, snapshot)
		}
		if snapshot.InFlight != 0 {
			t.Fatalf("station %d leaked in-flight admission: %#v", index, snapshot)
		}
	}
}

func TestAdaptivePoolRejectsRequestIDReuse(t *testing.T) {
	pool, err := StartAdaptivePool(
		muxHelperConfig("normal"),
		1,
		AdaptivePoolConfig{
			MaxInFlightPerStation: 2,
			InitialLatency:        time.Millisecond,
			EWMAAlpha:             0.2,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()

	if _, err := pool.Evaluate(
		context.Background(),
		"adaptive-once",
		muxTestRequest("adaptive-once-action", "adaptive-once-next"),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Evaluate(
		context.Background(),
		"adaptive-once",
		muxTestRequest("adaptive-second-action", "adaptive-second-next"),
	); err == nil {
		t.Fatal("expected adaptive pool request_id reuse to fail closed")
	}
}

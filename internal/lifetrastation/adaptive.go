package lifetrastation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/lifetrabridge"
)

var ErrBackpressure = errors.New("adaptive station pool backpressure")

type BackpressureError struct {
	InFlight   int
	Capacity   int
	RetryAfter time.Duration
}

func (err *BackpressureError) Error() string {
	return fmt.Sprintf(
		"%v: %d/%d control requests are in flight; retry after about %s",
		ErrBackpressure,
		err.InFlight,
		err.Capacity,
		err.RetryAfter,
	)
}

func (err *BackpressureError) Unwrap() error {
	return ErrBackpressure
}

type AdaptivePoolConfig struct {
	MaxInFlightPerStation int
	InitialLatency        time.Duration
	EWMAAlpha             float64
}

func (config AdaptivePoolConfig) normalized() (AdaptivePoolConfig, error) {
	if config.MaxInFlightPerStation < 1 {
		return AdaptivePoolConfig{}, errors.New("max in-flight per station must be positive")
	}
	if config.InitialLatency <= 0 {
		config.InitialLatency = 250 * time.Microsecond
	}
	if config.EWMAAlpha == 0 {
		config.EWMAAlpha = 0.20
	}
	if config.EWMAAlpha <= 0 || config.EWMAAlpha > 1 {
		return AdaptivePoolConfig{}, errors.New("EWMA alpha must be in (0, 1]")
	}
	return config, nil
}

type StationSnapshot struct {
	PID            int
	InFlight       int
	MaxInFlight    int
	Completed      uint64
	Errors         uint64
	EWMALatency    time.Duration
	EstimatedDelay time.Duration
}

type adaptiveStation struct {
	station   *MultiplexProcess
	inFlight  int
	completed uint64
	errors    uint64
	ewma      time.Duration
}

type AdaptivePool struct {
	config   AdaptivePoolConfig
	stations []*adaptiveStation

	mu     sync.Mutex
	seen   map[string]struct{}
	cursor int
}

func StartAdaptivePool(
	processConfig ProcessConfig,
	size int,
	adaptiveConfig AdaptivePoolConfig,
) (*AdaptivePool, error) {
	if size < 1 {
		return nil, errors.New("pool size must be positive")
	}
	configs := make([]ProcessConfig, size)
	for index := range configs {
		configs[index] = processConfig
	}
	return StartAdaptivePoolWithConfigs(configs, adaptiveConfig)
}

func StartAdaptivePoolWithConfigs(
	processConfigs []ProcessConfig,
	adaptiveConfig AdaptivePoolConfig,
) (*AdaptivePool, error) {
	if len(processConfigs) == 0 {
		return nil, errors.New("at least one station process config is required")
	}
	config, err := adaptiveConfig.normalized()
	if err != nil {
		return nil, err
	}

	stations := make([]*MultiplexProcess, 0, len(processConfigs))
	for index, processConfig := range processConfigs {
		station := &MultiplexProcess{Config: processConfig}
		if err := station.Start(); err != nil {
			for _, started := range stations {
				_ = started.Close()
			}
			return nil, fmt.Errorf("start adaptive station %d: %w", index, err)
		}
		stations = append(stations, station)
	}
	return newAdaptivePoolFromStations(stations, config), nil
}

func newAdaptivePoolFromStations(
	stations []*MultiplexProcess,
	config AdaptivePoolConfig,
) *AdaptivePool {
	states := make([]*adaptiveStation, 0, len(stations))
	for _, station := range stations {
		states = append(states, &adaptiveStation{station: station})
	}
	return &AdaptivePool{
		config:   config,
		stations: states,
		seen:     make(map[string]struct{}),
	}
}

func (pool *AdaptivePool) Evaluate(
	ctx context.Context,
	requestID string,
	request lifetrabridge.StationRequest,
) (lifetrabridge.Decision, error) {
	if requestID == "" {
		return lifetrabridge.Decision{}, errors.New("request_id is required")
	}
	if err := request.Validate(); err != nil {
		return lifetrabridge.Decision{}, fmt.Errorf("validate station request: %w", err)
	}

	state, err := pool.reserve(requestID)
	if err != nil {
		return lifetrabridge.Decision{}, err
	}

	started := time.Now()
	decision, evaluateErr := state.station.Evaluate(ctx, requestID, request)
	pool.finish(state, time.Since(started), evaluateErr)
	return decision, evaluateErr
}

func (pool *AdaptivePool) reserve(requestID string) (*adaptiveStation, error) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	if len(pool.stations) == 0 {
		return nil, errors.New("adaptive station pool is empty")
	}
	if _, duplicate := pool.seen[requestID]; duplicate {
		return nil, fmt.Errorf("request_id %q was already used by this adaptive pool", requestID)
	}

	index := pool.chooseStationLocked()
	if index < 0 {
		return nil, pool.backpressureLocked()
	}

	state := pool.stations[index]
	state.inFlight++
	pool.seen[requestID] = struct{}{}
	pool.cursor = (index + 1) % len(pool.stations)
	return state, nil
}

func (pool *AdaptivePool) chooseStationLocked() int {
	if len(pool.stations) == 0 {
		return -1
	}

	// Explore every idle station once before latency starts influencing routing.
	for offset := 0; offset < len(pool.stations); offset++ {
		index := (pool.cursor + offset) % len(pool.stations)
		state := pool.stations[index]
		if state.completed == 0 && state.inFlight == 0 {
			return index
		}
	}

	bestIndex := -1
	var bestScore time.Duration
	for offset := 0; offset < len(pool.stations); offset++ {
		index := (pool.cursor + offset) % len(pool.stations)
		state := pool.stations[index]
		if state.inFlight >= pool.config.MaxInFlightPerStation {
			continue
		}
		score := pool.estimatedDelayLocked(state)
		if bestIndex < 0 || score < bestScore {
			bestIndex = index
			bestScore = score
		}
	}
	return bestIndex
}

func (pool *AdaptivePool) estimatedDelayLocked(state *adaptiveStation) time.Duration {
	service := state.ewma
	if service <= 0 {
		service = pool.config.InitialLatency
	}
	return time.Duration(state.inFlight+1) * service
}

func (pool *AdaptivePool) backpressureLocked() error {
	inFlight := 0
	retryAfter := time.Duration(0)
	for _, state := range pool.stations {
		inFlight += state.inFlight
		service := state.ewma
		if service <= 0 {
			service = pool.config.InitialLatency
		}
		if retryAfter == 0 || service < retryAfter {
			retryAfter = service
		}
	}
	if retryAfter <= 0 {
		retryAfter = time.Microsecond
	}
	return &BackpressureError{
		InFlight:   inFlight,
		Capacity:   len(pool.stations) * pool.config.MaxInFlightPerStation,
		RetryAfter: retryAfter,
	}
}

func (pool *AdaptivePool) finish(state *adaptiveStation, elapsed time.Duration, evaluateErr error) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	if state.inFlight > 0 {
		state.inFlight--
	}
	state.completed++
	if evaluateErr != nil {
		state.errors++
	}
	if state.ewma <= 0 {
		state.ewma = elapsed
		return
	}
	alpha := pool.config.EWMAAlpha
	state.ewma = time.Duration(
		alpha*float64(elapsed) + (1-alpha)*float64(state.ewma),
	)
}

func (pool *AdaptivePool) Snapshots() []StationSnapshot {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	snapshots := make([]StationSnapshot, 0, len(pool.stations))
	for _, state := range pool.stations {
		snapshots = append(snapshots, StationSnapshot{
			PID:            state.station.PID(),
			InFlight:       state.inFlight,
			MaxInFlight:    pool.config.MaxInFlightPerStation,
			Completed:      state.completed,
			Errors:         state.errors,
			EWMALatency:    state.ewma,
			EstimatedDelay: pool.estimatedDelayLocked(state),
		})
	}
	return snapshots
}

func (pool *AdaptivePool) PIDs() []int {
	snapshots := pool.Snapshots()
	pids := make([]int, 0, len(snapshots))
	for _, snapshot := range snapshots {
		pids = append(pids, snapshot.PID)
	}
	return pids
}

func (pool *AdaptivePool) Close() error {
	pool.mu.Lock()
	stations := append([]*adaptiveStation(nil), pool.stations...)
	pool.stations = nil
	pool.mu.Unlock()

	var firstErr error
	for _, state := range stations {
		if err := state.station.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

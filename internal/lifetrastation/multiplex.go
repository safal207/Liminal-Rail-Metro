package lifetrastation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/lifetrabridge"
)

const (
	RequestEnvelopeProtocol  = "lifetra.station.request-envelope.v0.2"
	ResponseEnvelopeProtocol = "lifetra.station.response-envelope.v0.2"
	MuxErrorProtocol         = "lifetra.station.error.v0.2"
)

type RequestEnvelope struct {
	Protocol  string                       `json:"protocol"`
	RequestID string                       `json:"request_id"`
	Request   lifetrabridge.StationRequest `json:"request"`
}

type ResponseEnvelope struct {
	Protocol  string                     `json:"protocol"`
	RequestID string                     `json:"request_id"`
	Decision  lifetrabridge.Decision     `json:"decision"`
}

type MuxErrorEnvelope struct {
	Protocol  string `json:"protocol"`
	RequestID string `json:"request_id,omitempty"`
	Error     string `json:"error"`
}

type ProcessConfig struct {
	Command string
	Args    []string
	Dir     string
	Env     []string
	Timeout time.Duration
}

type muxResult struct {
	decision lifetrabridge.Decision
	err      error
}

type pendingRequest struct {
	request lifetrabridge.StationRequest
	result  chan muxResult
}

type MultiplexProcess struct {
	Config ProcessConfig

	mu        sync.Mutex
	writeMu   sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	scanner   *bufio.Scanner
	stderr    bytes.Buffer
	pending   map[string]pendingRequest
	seen      map[string]struct{}
	abandoned map[string]struct{}
	readerDone chan struct{}
	readerErr error
	closed    bool
	waited    bool
}

func (station *MultiplexProcess) Start() error {
	station.mu.Lock()
	defer station.mu.Unlock()

	if station.Config.Command == "" {
		return errors.New("station command is required")
	}
	if station.cmd != nil {
		return errors.New("multiplex station already started")
	}

	cmd := exec.Command(station.Config.Command, station.Config.Args...)
	cmd.Dir = station.Config.Dir
	if len(station.Config.Env) > 0 {
		cmd.Env = append(os.Environ(), station.Config.Env...)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("open multiplex station stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("open multiplex station stdout: %w", err)
	}
	cmd.Stderr = &station.stderr

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("start multiplex station: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	station.cmd = cmd
	station.stdin = stdin
	station.scanner = scanner
	station.pending = make(map[string]pendingRequest)
	station.seen = make(map[string]struct{})
	station.abandoned = make(map[string]struct{})
	station.readerDone = make(chan struct{})

	go station.readLoop()
	return nil
}

func (station *MultiplexProcess) PID() int {
	station.mu.Lock()
	defer station.mu.Unlock()
	if station.cmd == nil || station.cmd.Process == nil {
		return 0
	}
	return station.cmd.Process.Pid
}

func (station *MultiplexProcess) Evaluate(
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

	entry := pendingRequest{
		request: request,
		result:  make(chan muxResult, 1),
	}

	station.mu.Lock()
	if station.cmd == nil || station.stdin == nil || station.scanner == nil {
		station.mu.Unlock()
		return lifetrabridge.Decision{}, errors.New("multiplex station is not started")
	}
	if station.closed {
		err := station.readerErr
		station.mu.Unlock()
		if err != nil {
			return lifetrabridge.Decision{}, fmt.Errorf("multiplex station is closed: %w", err)
		}
		return lifetrabridge.Decision{}, errors.New("multiplex station is closed")
	}
	if _, exists := station.seen[requestID]; exists {
		station.mu.Unlock()
		return lifetrabridge.Decision{}, fmt.Errorf("request_id %q was already used by this station", requestID)
	}
	station.seen[requestID] = struct{}{}
	station.pending[requestID] = entry
	station.mu.Unlock()

	envelope := RequestEnvelope{
		Protocol:  RequestEnvelopeProtocol,
		RequestID: requestID,
		Request:   request,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		station.removePending(requestID, false)
		return lifetrabridge.Decision{}, fmt.Errorf("encode request envelope: %w", err)
	}
	payload = append(payload, '\n')

	station.writeMu.Lock()
	_, writeErr := station.stdin.Write(payload)
	station.writeMu.Unlock()
	if writeErr != nil {
		station.removePending(requestID, false)
		return lifetrabridge.Decision{}, fmt.Errorf("write multiplex station request: %w: %s", writeErr, station.stderr.String())
	}

	if station.Config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, station.Config.Timeout)
		defer cancel()
	}

	select {
	case result := <-entry.result:
		return result.decision, result.err
	case <-ctx.Done():
		station.removePending(requestID, true)
		return lifetrabridge.Decision{}, fmt.Errorf("multiplex station response %q: %w", requestID, ctx.Err())
	}
}

func (station *MultiplexProcess) readLoop() {
	defer close(station.readerDone)

	for station.scanner.Scan() {
		line := append([]byte(nil), station.scanner.Bytes()...)
		if err := station.dispatchLine(line); err != nil {
			station.failFatal(err)
			return
		}
	}

	err := station.scanner.Err()
	if err == nil {
		err = io.EOF
	}
	station.failFatal(fmt.Errorf("multiplex station output closed: %w: %s", err, station.stderr.String()))
}

func (station *MultiplexProcess) dispatchLine(line []byte) error {
	var header struct {
		Protocol  string `json:"protocol"`
		RequestID string `json:"request_id"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(line, &header); err != nil {
		return fmt.Errorf("decode multiplex response header: %w", err)
	}

	station.mu.Lock()
	if _, abandoned := station.abandoned[header.RequestID]; abandoned {
		delete(station.abandoned, header.RequestID)
		station.mu.Unlock()
		return nil
	}
	entry, exists := station.pending[header.RequestID]
	if exists {
		delete(station.pending, header.RequestID)
	}
	station.mu.Unlock()

	if header.RequestID == "" {
		return fmt.Errorf("station emitted unbound %q response", header.Protocol)
	}
	if !exists {
		return fmt.Errorf("station emitted unknown request_id %q", header.RequestID)
	}

	switch header.Protocol {
	case ResponseEnvelopeProtocol:
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		var envelope ResponseEnvelope
		if err := decoder.Decode(&envelope); err != nil {
			entry.result <- muxResult{err: fmt.Errorf("decode response envelope: %w", err)}
			return nil
		}
		if err := requireJSONEOF(decoder); err != nil {
			entry.result <- muxResult{err: err}
			return nil
		}
		if envelope.RequestID != header.RequestID {
			entry.result <- muxResult{err: errors.New("response envelope request_id mismatch")}
			return nil
		}
		if err := validateBinding(entry.request, envelope.Decision); err != nil {
			entry.result <- muxResult{err: err}
			return nil
		}
		entry.result <- muxResult{decision: envelope.Decision}
		return nil

	case MuxErrorProtocol:
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		var envelope MuxErrorEnvelope
		if err := decoder.Decode(&envelope); err != nil {
			entry.result <- muxResult{err: fmt.Errorf("decode station error envelope: %w", err)}
			return nil
		}
		if err := requireJSONEOF(decoder); err != nil {
			entry.result <- muxResult{err: err}
			return nil
		}
		if envelope.Error == "" {
			envelope.Error = "multiplex Lifetra station returned an unspecified error"
		}
		entry.result <- muxResult{err: errors.New(envelope.Error)}
		return nil

	default:
		entry.result <- muxResult{err: fmt.Errorf("unexpected multiplex response protocol %q", header.Protocol)}
		return nil
	}
}

func (station *MultiplexProcess) removePending(requestID string, abandon bool) {
	station.mu.Lock()
	delete(station.pending, requestID)
	if abandon {
		station.abandoned[requestID] = struct{}{}
	}
	station.mu.Unlock()
}

func (station *MultiplexProcess) failFatal(err error) {
	station.mu.Lock()
	if station.closed {
		station.mu.Unlock()
		return
	}
	station.closed = true
	station.readerErr = err
	pending := station.pending
	station.pending = make(map[string]pendingRequest)
	cmd := station.cmd
	station.mu.Unlock()

	for _, entry := range pending {
		entry.result <- muxResult{err: err}
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func (station *MultiplexProcess) Close() error {
	station.writeMu.Lock()
	station.mu.Lock()
	if station.cmd == nil {
		station.mu.Unlock()
		station.writeMu.Unlock()
		return nil
	}
	if !station.closed {
		station.closed = true
	}
	stdin := station.stdin
	cmd := station.cmd
	readerDone := station.readerDone
	waited := station.waited
	station.mu.Unlock()

	if stdin != nil {
		_ = stdin.Close()
	}
	station.writeMu.Unlock()

	if readerDone != nil {
		<-readerDone
	}

	station.mu.Lock()
	defer station.mu.Unlock()
	if waited || station.waited {
		return nil
	}
	station.waited = true
	if err := cmd.Wait(); err != nil && station.readerErr == nil {
		return fmt.Errorf("wait for multiplex station: %w: %s", err, station.stderr.String())
	}
	return nil
}

type Pool struct {
	stations []*MultiplexProcess
	next     atomic.Uint64
}

func StartPool(config ProcessConfig, size int) (*Pool, error) {
	if size < 1 {
		return nil, errors.New("pool size must be positive")
	}

	pool := &Pool{stations: make([]*MultiplexProcess, 0, size)}
	for index := 0; index < size; index++ {
		station := &MultiplexProcess{Config: config}
		if err := station.Start(); err != nil {
			_ = pool.Close()
			return nil, fmt.Errorf("start station %d: %w", index, err)
		}
		pool.stations = append(pool.stations, station)
	}
	return pool, nil
}

func (pool *Pool) Evaluate(
	ctx context.Context,
	requestID string,
	request lifetrabridge.StationRequest,
) (lifetrabridge.Decision, error) {
	if len(pool.stations) == 0 {
		return lifetrabridge.Decision{}, errors.New("station pool is empty")
	}
	index := pool.next.Add(1) - 1
	station := pool.stations[index%uint64(len(pool.stations))]
	return station.Evaluate(ctx, requestID, request)
}

func (pool *Pool) PIDs() []int {
	pids := make([]int, 0, len(pool.stations))
	for _, station := range pool.stations {
		pids = append(pids, station.PID())
	}
	return pids
}

func (pool *Pool) Close() error {
	var firstErr error
	for _, station := range pool.stations {
		if err := station.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

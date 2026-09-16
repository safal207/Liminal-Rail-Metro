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
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/lifetrabridge"
)

const StationErrorProtocol = "lifetra.station.error.v0.1"

type PersistentProcess struct {
	Command string
	Args    []string
	Dir     string
	Env     []string
	Timeout time.Duration

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	stderr  bytes.Buffer
	closed  bool
	waited  bool
}

func (station *PersistentProcess) Start() error {
	station.mu.Lock()
	defer station.mu.Unlock()

	if station.Command == "" {
		return errors.New("station command is required")
	}
	if station.cmd != nil {
		return errors.New("persistent station already started")
	}

	cmd := exec.Command(station.Command, station.Args...)
	cmd.Dir = station.Dir
	if len(station.Env) > 0 {
		cmd.Env = append(os.Environ(), station.Env...)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("open station stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("open station stdout: %w", err)
	}
	cmd.Stderr = &station.stderr

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("start persistent station: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	station.cmd = cmd
	station.stdin = stdin
	station.scanner = scanner
	return nil
}

func (station *PersistentProcess) PID() int {
	station.mu.Lock()
	defer station.mu.Unlock()
	if station.cmd == nil || station.cmd.Process == nil {
		return 0
	}
	return station.cmd.Process.Pid
}

func (station *PersistentProcess) Evaluate(ctx context.Context, request lifetrabridge.StationRequest) (lifetrabridge.Decision, error) {
	station.mu.Lock()
	defer station.mu.Unlock()

	if err := request.Validate(); err != nil {
		return lifetrabridge.Decision{}, fmt.Errorf("validate station request: %w", err)
	}
	if station.cmd == nil || station.stdin == nil || station.scanner == nil {
		return lifetrabridge.Decision{}, errors.New("persistent station is not started")
	}
	if station.closed {
		return lifetrabridge.Decision{}, errors.New("persistent station is closed")
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return lifetrabridge.Decision{}, fmt.Errorf("encode station request: %w", err)
	}
	payload = append(payload, '\n')

	if _, err := station.stdin.Write(payload); err != nil {
		return lifetrabridge.Decision{}, fmt.Errorf("write station request: %w: %s", err, station.stderr.String())
	}

	if station.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, station.Timeout)
		defer cancel()
	}

	type scanResult struct {
		line []byte
		err  error
	}
	resultCh := make(chan scanResult, 1)
	go func() {
		if station.scanner.Scan() {
			line := append([]byte(nil), station.scanner.Bytes()...)
			resultCh <- scanResult{line: line}
			return
		}
		err := station.scanner.Err()
		if err == nil {
			err = io.EOF
		}
		resultCh <- scanResult{err: err}
	}()

	var scanned scanResult
	select {
	case scanned = <-resultCh:
	case <-ctx.Done():
		station.closed = true
		if station.stdin != nil {
			_ = station.stdin.Close()
		}
		if station.cmd.Process != nil {
			_ = station.cmd.Process.Kill()
		}
		if !station.waited {
			_ = station.cmd.Wait()
			station.waited = true
		}
		return lifetrabridge.Decision{}, fmt.Errorf("persistent station response: %w", ctx.Err())
	}

	if scanned.err != nil {
		return lifetrabridge.Decision{}, fmt.Errorf("read station response: %w: %s", scanned.err, station.stderr.String())
	}

	var header struct {
		Protocol string `json:"protocol"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal(scanned.line, &header); err != nil {
		return lifetrabridge.Decision{}, fmt.Errorf("decode station response header: %w", err)
	}
	if header.Protocol == StationErrorProtocol {
		if header.Error == "" {
			header.Error = "persistent Lifetra station returned an unspecified error"
		}
		return lifetrabridge.Decision{}, errors.New(header.Error)
	}

	decoder := json.NewDecoder(bytes.NewReader(scanned.line))
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

func (station *PersistentProcess) Close() error {
	station.mu.Lock()
	defer station.mu.Unlock()

	if station.cmd == nil {
		return nil
	}
	station.closed = true
	if station.waited {
		return nil
	}

	if station.stdin != nil {
		_ = station.stdin.Close()
	}
	station.waited = true
	if err := station.cmd.Wait(); err != nil {
		return fmt.Errorf("wait for persistent station: %w: %s", err, station.stderr.String())
	}
	return nil
}

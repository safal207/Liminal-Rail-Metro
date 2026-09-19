package codingworkflow

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type logBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	truncated bool
}

func (b *logBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(data)
	remaining := MaxLogBytes - b.buf.Len()
	if len(data) > remaining {
		b.truncated = true
		data = data[:remaining]
	}
	_, _ = b.buf.Write(data)
	return n, nil
}
func (b *logBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buf.String() }

// The process gets a narrow environment, but tests are still executable code.
// No environment filtering substitutes for the caller's OS/container sandbox.
func execute(ctx context.Context, root string, c Check) Check {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	c.ExitCode = -1
	tmp, err := os.MkdirTemp("", "metro-check-env-")
	if err != nil {
		return finishCheck(c, "", "runner environment unavailable")
	}
	defer os.RemoveAll(tmp)
	goBin, err := exec.LookPath("go")
	if err != nil {
		return finishCheck(c, "", "Go toolchain unavailable")
	}
	goBin, err = filepath.Abs(goBin)
	if err != nil {
		return finishCheck(c, "", "Go toolchain unavailable")
	}
	modCache := os.Getenv("GOMODCACHE")
	if modCache == "" {
		home, _ := os.UserHomeDir()
		modCache = filepath.Join(home, "go", "pkg", "mod")
	}
	cache := os.Getenv("GOCACHE")
	if cache == "" {
		cache = filepath.Join(tmp, "go-cache")
	}
	for _, path := range []string{modCache, cache, goBin} {
		absRoot, _ := filepath.Abs(root)
		absPath, _ := filepath.Abs(path)
		if absPath == absRoot || strings.HasPrefix(absPath, absRoot+string(os.PathSeparator)) {
			return finishCheck(c, "", "toolchain and caches must be outside candidate checkout")
		}
	}
	cmd := exec.CommandContext(ctx, goBin, c.Args...)
	cmd.Dir = root
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + tmp, "TMPDIR=" + tmp, "GOCACHE=" + cache,
		"GOMODCACHE=" + modCache, "GOENV=off", "GOWORK=off", "GOFLAGS=-mod=readonly", "GOTOOLCHAIN=local", "GOPROXY=off", "LC_ALL=C"}
	cmd.WaitDelay = time.Second
	configureProcess(cmd)
	var out, errs logBuffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Start(); err != nil {
		return finishCheck(c, "", "test process failed to start")
	}
	c.Started = true
	err = cmd.Wait()
	c.TimedOut = ctx.Err() != nil
	c.Truncated = out.truncated || errs.truncated
	if err == nil {
		c.ExitCode = 0
	} else if cmd.ProcessState != nil {
		c.ExitCode = cmd.ProcessState.ExitCode()
	}
	return finishCheck(c, out.String(), errs.String())
}

func finishCheck(c Check, out, errs string) Check {
	c.Stdout, c.Stderr = out, errs
	c.StdoutSHA256, c.StderrSHA256 = Hash([]byte(out)), Hash([]byte(errs))
	return c
}

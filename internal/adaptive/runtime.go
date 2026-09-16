package adaptive

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// HostState is intentionally small, read-only telemetry available without
// privileged hardware access.
type HostState struct {
	ObservedAt      string  `json:"observed_at"`
	LogicalCPUs     int     `json:"logical_cpus"`
	GOMAXPROCS      int     `json:"gomaxprocs"`
	Goroutines      int     `json:"goroutines"`
	HeapAllocMiB    float64 `json:"heap_alloc_mib"`
	Load1           float64 `json:"load_1,omitempty"`
	MemAvailableMiB float64 `json:"mem_available_mib,omitempty"`
}

// SenseHost returns bounded, read-only telemetry. Linux-specific values are
// best-effort; the runtime fields work on every Go platform.
func SenseHost() HostState {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	state := HostState{
		ObservedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		LogicalCPUs:  runtime.NumCPU(),
		GOMAXPROCS:   runtime.GOMAXPROCS(0),
		Goroutines:   runtime.NumGoroutine(),
		HeapAllocMiB: float64(ms.HeapAlloc) / (1024 * 1024),
	}
	state.Load1 = readLoad1()
	state.MemAvailableMiB = readMemAvailableMiB()
	return state
}

func readLoad1() float64 {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

func readMemAvailableMiB() float64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "MemAvailable:" {
			kb, err := strconv.ParseFloat(fields[1], 64)
			if err == nil {
				return kb / 1024
			}
		}
	}
	return 0
}

package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// diskMetric reports root-filesystem usage. On collection failure Error is set
// and the other fields are left zero.
type diskMetric struct {
	Used  string `json:"used,omitempty"`
	Total string `json:"total,omitempty"`
	Pct   int    `json:"pct"`
	Error string `json:"error,omitempty"`
}

// memoryMetric reports RAM usage in MB. On collection failure Error is set.
type memoryMetric struct {
	UsedMB  int64  `json:"used_mb"`
	TotalMB int64  `json:"total_mb"`
	Pct     int    `json:"pct"`
	Error   string `json:"error,omitempty"`
}

// metricsResult is the response shape for the metrics.collect RPC.
type metricsResult struct {
	Disk      diskMetric   `json:"disk"`
	Memory    memoryMetric `json:"memory"`
	Load      []float64    `json:"load,omitempty"`
	LoadError string       `json:"load_error,omitempty"`
}

// MetricsCollect is the metrics.collect RPC handler. It takes no request
// payload and returns lightweight system metrics (disk, memory, load average)
// gathered via /proc reads and a single `df` exec. A failure collecting any
// one metric is reported in that metric's error field rather than failing the
// whole response.
func MetricsCollect(_ context.Context, params json.RawMessage) (any, error) {
	// Empty payload is expected; only reject genuinely malformed JSON.
	if len(strings.TrimSpace(string(params))) > 0 {
		var ignored map[string]any
		if err := json.Unmarshal(params, &ignored); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
	}

	result := metricsResult{}

	if out, err := exec.Command("df", "-h", "/").Output(); err != nil {
		result.Disk = diskMetric{Error: fmt.Sprintf("df failed: %v", err)}
	} else {
		result.Disk = parseDisk(string(out))
	}

	if data, err := os.ReadFile("/proc/meminfo"); err != nil {
		result.Memory = memoryMetric{Error: fmt.Sprintf("could not read /proc/meminfo: %v", err)}
	} else {
		result.Memory = parseMemory(string(data))
	}

	if data, err := os.ReadFile("/proc/loadavg"); err != nil {
		result.LoadError = fmt.Sprintf("could not read /proc/loadavg: %v", err)
	} else {
		load, loadErr := parseLoad(string(data))
		result.Load = load
		result.LoadError = loadErr
	}

	return result, nil
}

// parseDisk parses `df -h /` output, extracting the data line for the root
// mount. df -h columns: Filesystem Size Used Avail Use% Mounted-on.
func parseDisk(dfOutput string) diskMetric {
	lines := strings.Split(strings.TrimSpace(dfOutput), "\n")
	if len(lines) < 2 {
		return diskMetric{Error: "unexpected df output"}
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return diskMetric{Error: "unexpected df format"}
	}

	pct, err := strconv.Atoi(strings.TrimSuffix(fields[4], "%"))
	if err != nil {
		return diskMetric{Error: fmt.Sprintf("could not parse disk percent %q", fields[4])}
	}

	return diskMetric{
		Used:  fields[2],
		Total: fields[1],
		Pct:   pct,
	}
}

// parseMemory parses /proc/meminfo, computing used/total MB and percent from
// MemTotal and MemAvailable (both reported in kB).
func parseMemory(meminfo string) memoryMetric {
	var memTotalKB, memAvailKB int64
	scanner := bufio.NewScanner(strings.NewReader(meminfo))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			memTotalKB = parseMeminfoValue(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			memAvailKB = parseMeminfoValue(line)
		}
	}

	if memTotalKB == 0 {
		return memoryMetric{Error: "could not parse MemTotal from /proc/meminfo"}
	}

	usedKB := memTotalKB - memAvailKB
	return memoryMetric{
		UsedMB:  usedKB / 1024,
		TotalMB: memTotalKB / 1024,
		Pct:     int(float64(usedKB) / float64(memTotalKB) * 100),
	}
}

// parseMeminfoValue extracts the kB value from a /proc/meminfo line such as
// "MemTotal:        1900000 kB".
func parseMeminfoValue(line string) int64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	val, _ := strconv.ParseInt(fields[1], 10, 64)
	return val
}

// parseLoad parses /proc/loadavg, returning the 1m/5m/15m load averages. The
// returned string is non-empty only on a parse failure.
func parseLoad(loadavg string) ([]float64, string) {
	fields := strings.Fields(loadavg)
	if len(fields) < 3 {
		return nil, "could not parse load average from /proc/loadavg"
	}

	load := make([]float64, 3)
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return nil, fmt.Sprintf("could not parse load value %q", fields[i])
		}
		load[i] = v
	}
	return load, ""
}

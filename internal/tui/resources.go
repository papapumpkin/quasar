package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// ResourceLevel classifies the severity of resource usage.
type ResourceLevel int

const (
	// ResourceNormal indicates resource usage within safe bounds.
	ResourceNormal ResourceLevel = iota
	// ResourceWarning indicates elevated resource usage.
	ResourceWarning
	// ResourceDanger indicates dangerously high resource usage.
	ResourceDanger
)

// ResourceThresholds defines the thresholds for resource warning/danger levels.
type ResourceThresholds struct {
	CPUWarningPercent float64
	CPUDangerPercent  float64
	MemoryWarningMB   float64
	MemoryDangerMB    float64
}

// DefaultResourceThresholds returns reasonable defaults for resource monitoring.
func DefaultResourceThresholds() ResourceThresholds {
	return ResourceThresholds{
		CPUWarningPercent: 50,
		CPUDangerPercent:  80,
		MemoryWarningMB:   1024,
		MemoryDangerMB:    2048,
	}
}

// ResourceSnapshot captures a point-in-time view of resource consumption
// for the quasar process tree.
type ResourceSnapshot struct {
	CPUPercent   float64 // total CPU% of quasar + children
	MemoryMB     float64 // total RSS of quasar + children
	NumProcesses int     // count of processes in the group
	QuasarCount  int     // number of quasar processes running system-wide
}

// CPULevel returns the resource level for CPU usage.
func (s ResourceSnapshot) CPULevel(t ResourceThresholds) ResourceLevel {
	switch {
	case s.CPUPercent >= t.CPUDangerPercent:
		return ResourceDanger
	case s.CPUPercent >= t.CPUWarningPercent:
		return ResourceWarning
	default:
		return ResourceNormal
	}
}

// MemoryLevel returns the resource level for memory usage.
func (s ResourceSnapshot) MemoryLevel(t ResourceThresholds) ResourceLevel {
	switch {
	case s.MemoryMB >= t.MemoryDangerMB:
		return ResourceDanger
	case s.MemoryMB >= t.MemoryWarningMB:
		return ResourceWarning
	default:
		return ResourceNormal
	}
}

// WorstLevel returns the highest severity level across CPU and memory.
func (s ResourceSnapshot) WorstLevel(t ResourceThresholds) ResourceLevel {
	cpu := s.CPULevel(t)
	mem := s.MemoryLevel(t)
	if cpu > mem {
		return cpu
	}
	return mem
}

// SampleResources collects resource usage for the given PID and its children.
// It gracefully returns a zero-valued snapshot if sampling fails.
func SampleResources(ctx context.Context, pid int) ResourceSnapshot {
	snap := sampleProcessGroup(ctx, pid)
	snap.QuasarCount = countQuasarProcesses(ctx)
	return snap
}

// sampleProcessGroup uses `ps` to collect CPU and memory stats for a process group.
//
// On macOS, -g (process group) accurately captures the quasar process tree.
// On Linux, we combine `ps -p <pid>` (for the parent) with `pgrep -P <pid>`
// to enumerate direct children, avoiding the session-ID approach (`ps --sid`)
// which can overcount by including unrelated processes in the same terminal session.
func sampleProcessGroup(ctx context.Context, pid int) ResourceSnapshot {
	switch runtime.GOOS {
	case "darwin":
		// macOS: use -g to get the process group.
		pgid := pid // on macOS, the PGID of the leader is its own PID
		args := []string{"-o", "pid=,rss=,%cpu=", "-g", strconv.Itoa(pgid)}
		out, err := exec.CommandContext(ctx, "ps", args...).Output()
		if err != nil {
			return ResourceSnapshot{}
		}
		return parsePSOutput(string(out))
	case "linux":
		// Linux: collect the parent process and its direct children.
		// First, get child PIDs via pgrep -P (parent PID matching).
		childOut, _ := exec.CommandContext(ctx, "pgrep", "-P", strconv.Itoa(pid)).Output()
		// Build a list of PIDs: parent + children.
		pids := []string{strconv.Itoa(pid)}
		for _, line := range strings.Split(strings.TrimSpace(string(childOut)), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				pids = append(pids, line)
			}
		}
		args := append([]string{"-o", "pid=,rss=,%cpu=", "-p"}, strings.Join(pids, ","))
		out, err := exec.CommandContext(ctx, "ps", args...).Output()
		if err != nil {
			return ResourceSnapshot{}
		}
		return parsePSOutput(string(out))
	default:
		// Unsupported platform — return empty snapshot.
		return ResourceSnapshot{}
	}
}

// parsePSOutput parses the output of `ps -o pid=,rss=,%cpu=` and aggregates
// the total RSS (converted to MB) and CPU% across all listed processes.
func parsePSOutput(output string) ResourceSnapshot {
	var snap ResourceSnapshot
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		// fields[0] = PID (unused but present), fields[1] = RSS (KB), fields[2] = %CPU
		rssKB, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			continue
		}
		cpu, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			continue
		}
		snap.MemoryMB += rssKB / 1024.0
		snap.CPUPercent += cpu
		snap.NumProcesses++
	}
	return snap
}

// countQuasarProcesses counts the number of running quasar processes system-wide.
// Uses -x for exact process name matching to avoid false positives from partial
// matches (e.g., "quasar-backup.sh"). Note that pgrep exits with code 1 when
// there are zero matches, which is treated as a non-error (returns 0).
func countQuasarProcesses(ctx context.Context) int {
	out, err := exec.CommandContext(ctx, "pgrep", "-xc", "quasar").Output()
	if err != nil {
		// pgrep exits 1 when no processes match — this is not an error for us.
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0
	}
	return n
}

// FormatResourceIndicator renders a compact resource string for the status bar.
// Format: "◈2  48MB  3.2%"
// Returns empty string if no snapshot data is available.
func FormatResourceIndicator(snap ResourceSnapshot) string {
	if snap.NumProcesses == 0 && snap.MemoryMB == 0 && snap.CPUPercent == 0 {
		return ""
	}

	var parts []string

	// Process count.
	parts = append(parts, fmt.Sprintf("◈%d", snap.NumProcesses))

	// Memory — compact format.
	if snap.MemoryMB >= 1024 {
		parts = append(parts, fmt.Sprintf("%.1fGB", snap.MemoryMB/1024.0))
	} else {
		parts = append(parts, fmt.Sprintf("%.0fMB", snap.MemoryMB))
	}

	// CPU.
	parts = append(parts, fmt.Sprintf("%.1f%%", snap.CPUPercent))

	return strings.Join(parts, "  ")
}

// FormatQuasarCount renders the multi-quasar indicator.
// Returns empty string if count <= 1.
func FormatQuasarCount(count int) string {
	if count <= 1 {
		return ""
	}
	return fmt.Sprintf("⚡%d quasars", count)
}

// SampleResourcesFromSelf is a convenience that calls SampleResources with the current PID.
func SampleResourcesFromSelf(ctx context.Context) ResourceSnapshot {
	return SampleResources(ctx, os.Getpid())
}

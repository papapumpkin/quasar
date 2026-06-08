package claude

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/papapumpkin/quasar/internal/agent"
)

// excludedWatchDirs are directory base names never watched for file-write
// activity: they churn constantly (or are huge) and a write there is not a
// signal of coder progress.
var excludedWatchDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
}

// fileWriteWatcher tracks the most recent write under a workdir using fsnotify.
// IdleSince reports how long the tree has been quiet, which feeds the
// write-idle signal. It is safe for concurrent use.
type fileWriteWatcher struct {
	watcher *fsnotify.Watcher
	clock   func() time.Time

	mu        sync.Mutex
	lastWrite time.Time
}

// newFileWriteWatcher starts watching workdir recursively (excluding churny
// dirs) and returns a watcher whose idle timer resets on every write or create
// event. The caller must Close it. The clock is injectable for tests.
func newFileWriteWatcher(workdir string, clock func() time.Time) (*fileWriteWatcher, error) {
	if clock == nil {
		clock = time.Now
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	fw := &fileWriteWatcher{watcher: w, clock: clock, lastWrite: clock()}

	if err := fw.addTree(workdir); err != nil {
		_ = w.Close()
		return nil, err
	}

	go fw.loop()
	return fw, nil
}

// addTree adds workdir and all its (non-excluded) subdirectories to the watch.
func (fw *fileWriteWatcher) addTree(workdir string) error {
	return filepath.WalkDir(workdir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries rather than aborting
		}
		if !d.IsDir() {
			return nil
		}
		if excludedWatchDirs[d.Name()] && path != workdir {
			return filepath.SkipDir
		}
		_ = fw.watcher.Add(path)
		return nil
	})
}

// loop consumes fsnotify events, resetting the idle timer on writes and adding
// newly created directories to the watch so deep writes keep counting.
func (fw *fileWriteWatcher) loop() {
	for ev := range fw.watcher.Events {
		base := filepath.Base(filepath.Dir(ev.Name))
		if excludedWatchDirs[base] {
			continue
		}
		if ev.Op&(fsnotify.Write|fsnotify.Create) != 0 {
			fw.touch()
			if ev.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() && !excludedWatchDirs[filepath.Base(ev.Name)] {
					_ = fw.watcher.Add(ev.Name)
				}
			}
		}
	}
}

// touch records the current time as the last write.
func (fw *fileWriteWatcher) touch() {
	fw.mu.Lock()
	fw.lastWrite = fw.clock()
	fw.mu.Unlock()
}

// IdleSince returns how long it has been since the last write, and true (the
// watcher is always valid once started — its baseline is its start time).
func (fw *fileWriteWatcher) IdleSince(now time.Time) (time.Duration, bool) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	return now.Sub(fw.lastWrite), true
}

// Close stops the watcher and its goroutine.
func (fw *fileWriteWatcher) Close() error {
	return fw.watcher.Close()
}

// tokenSample is a timestamped output-token delta from the claude stream.
type tokenSample struct {
	t      time.Time
	tokens int
}

// NOT YET WIRED INTO THE LIVE INVOCATION: the token-rate signal source below
// (tokenRateMeter, parseTokenCount, streamEvent) is unit-tested but has no
// production caller yet. Feeding it requires switching the CLI to
// --output-format stream-json (claude.go still uses buffered json), which
// rewrites the final-result parse path. runMonitored therefore leaves
// Healthcheck.tokenRateFn nil (valid=false → the signal is ignored), so the
// active probe is write-idle + cpu-idle + wall-clock. When stream-json lands,
// wire a *tokenRateMeter to tokenRateFn and feed it from the event stream —
// observing the CONTRACT documented on Observe below. Tracked as follow-up
// "enable stream-json token-rate signal".

// tokenRateMeter computes a sliding-window token rate (tokens/sec) from
// per-event output-token *deltas*. It is the source for the token-rate signal.
// Until Observe is called at least once it reports valid=false, so a coder that
// has not yet produced any output is never judged "stuck reasoning".
type tokenRateMeter struct {
	window time.Duration
	clock  func() time.Time

	mu      sync.Mutex
	samples []tokenSample
	seen    bool
}

// newTokenRateMeter returns a meter averaging over window.
func newTokenRateMeter(window time.Duration, clock func() time.Time) *tokenRateMeter {
	if clock == nil {
		clock = time.Now
	}
	if window <= 0 {
		window = agent.DefaultTokenRateWindow
	}
	return &tokenRateMeter{window: window, clock: clock}
}

// Observe records delta output tokens produced now.
//
// CONTRACT: delta must be the INCREMENTAL output-token count since the previous
// Observe, not a running total. parseTokenCount returns claude's
// usage.output_tokens, which in stream-json is CUMULATIVE per message (and the
// final result event reports the grand total). The caller wiring this meter
// must therefore track the last-seen cumulative value per stream and Observe
// the difference (current - lastSeen) — feeding the raw cumulative counts here
// would sum totals and massively overstate the rate.
func (m *tokenRateMeter) Observe(delta int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seen = true
	m.samples = append(m.samples, tokenSample{t: m.clock(), tokens: delta})
}

// Rate returns the tokens/sec averaged over the window ending at now, and
// whether the meter has any data yet. Samples older than the window are pruned.
func (m *tokenRateMeter) Rate(now time.Time) (float64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.seen {
		return 0, false
	}
	cutoff := now.Add(-m.window)
	kept := m.samples[:0]
	var sum int
	for _, s := range m.samples {
		if s.t.Before(cutoff) {
			continue
		}
		kept = append(kept, s)
		sum += s.tokens
	}
	m.samples = kept
	return float64(sum) / m.window.Seconds(), true
}

// streamEvent is the subset of a claude stream-json event we parse for token
// accounting. The CLI nests usage under "message" for assistant events and may
// also surface a top-level "usage" on the final result.
type streamEvent struct {
	Usage   *Usage `json:"usage"`
	Message *struct {
		Usage *Usage `json:"usage"`
	} `json:"message"`
}

// parseTokenCount extracts the output-token count from one stream-json line.
// It returns (count, true) when a usage block with output tokens is present.
func parseTokenCount(line []byte) (int, bool) {
	line = []byte(strings.TrimSpace(string(line)))
	if len(line) == 0 || line[0] != '{' {
		return 0, false
	}
	var ev streamEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return 0, false
	}
	if ev.Message != nil && ev.Message.Usage != nil && ev.Message.Usage.OutputTokens > 0 {
		return ev.Message.Usage.OutputTokens, true
	}
	if ev.Usage != nil && ev.Usage.OutputTokens > 0 {
		return ev.Usage.OutputTokens, true
	}
	return 0, false
}

// cpuPoller tracks the last time a subprocess showed CPU activity (≥1%).
// IdleSince feeds the cpu-idle signal: a process pinned at 0% for too long is
// blocked on something that will not return. The ps invocation is injectable so
// tests need no real process.
type cpuPoller struct {
	pid   int
	clock func() time.Time
	// readPCPU returns the process CPU percentage. Defaults to a `ps` call.
	readPCPU func(pid int) (float64, error)

	mu         sync.Mutex
	lastActive time.Time
	seen       bool
}

// newCPUPoller returns a poller for pid, with the last-active baseline set to
// the start time so a process that never spins is eventually flagged.
func newCPUPoller(pid int, clock func() time.Time) *cpuPoller {
	if clock == nil {
		clock = time.Now
	}
	return &cpuPoller{
		pid:        pid,
		clock:      clock,
		readPCPU:   psPercentCPU,
		lastActive: clock(),
	}
}

// Poll samples CPU once and resets the idle timer when the process is active.
func (p *cpuPoller) Poll() {
	pct, err := p.readPCPU(p.pid)
	if err != nil {
		return // transient ps error: treat as no new info, keep prior timer
	}
	p.mu.Lock()
	p.seen = true
	if pct >= 1.0 {
		p.lastActive = p.clock()
	}
	p.mu.Unlock()
}

// IdleSince returns how long the process has sat below 1% CPU, and whether at
// least one successful sample has been taken.
func (p *cpuPoller) IdleSince(now time.Time) (time.Duration, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.seen {
		return 0, false
	}
	return now.Sub(p.lastActive), true
}

// psPercentCPU shells out to `ps -o pcpu= -p <pid>` and parses the percentage.
func psPercentCPU(pid int) (float64, error) {
	out, err := exec.Command("ps", "-o", "pcpu=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, err
	}
	field := strings.TrimSpace(string(out))
	if field == "" {
		return 0, nil
	}
	return strconv.ParseFloat(field, 64)
}

// runCPUPoller polls the CPU at interval until ctx is canceled. It is started
// by the Invoker alongside the healthcheck.
func runCPUPoller(ctx context.Context, p *cpuPoller, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultTick
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	p.Poll()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.Poll()
		}
	}
}

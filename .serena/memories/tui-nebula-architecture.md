# Quasar TUI and Nebula System - Architecture Deep Dive

## Overview
Quasar is a dual-agent coder-reviewer loop orchestrator that supports both single-task (loop mode) and multi-task (nebula mode) execution. The TUI is powered by BubbleTea and uses a bridge pattern to convert imperative UI calls into typed messages sent asynchronously to the BubbleTea program.

---

## TUI Architecture

### Core Components

**File: `internal/tui/tui.go`**
- `NewProgram(mode Mode, noSplash bool, opts ...tea.ProgramOption) *Program` — Creates a BubbleTea program with alternate screen buffer
- `NewProgramRaw(mode Mode) *Program` — Raw program factory
- `NewNebulaProgram(name string, phases []PhaseInfo, nebulaDir string, noSplash bool) *Program` — Pre-populates nebula mode with phase table
- `Run(mode Mode, noSplash bool) error` — Blocking run method
- `WithOutput(w io.Writer) tea.ProgramOption` — Output redirection for testing

**File: `internal/tui/model.go`**
- `AppModel` — Root BubbleTea model composing all sub-views
  - `Mode Mode` — Loop or Nebula mode
  - `StatusBar StatusBar` — Top status line
  - `Banner Banner` — Title banner
  - `LoopView LoopView` — Single-task view
  - `NebulaView NebulaView` — Multi-task phase table
  - `Detail DetailPanel` — Right-side detail/drill-down panel
  - `Gate *GatePrompt` — Human gate decision overlay
  - `Overlay *CompletionOverlay` — Completion and next-nebula picker
  - `Splash *SplashModel` — Binary-star startup animation (nil if disabled)
  - Navigation state: `Depth ViewDepth` (Phases → PhaseLoop → AgentOutput)
  - `PhaseLoops map[string]*LoopView` — Per-phase cycle timelines
  - Execution control: `Paused bool`, `Stopping bool`, `NebulaDir string`
- `NewAppModel(mode Mode) AppModel` — Creates model with splash enabled by default
- `DisableSplash()` — Clears splash for immediate main view (--no-splash)
- `Init() tea.Cmd` — Starts spinners, tick timer, resource sampler, and splash animation

### Views

**`NebulaView`** (`internal/tui/nebulaview.go`)
- Renders phase table with status icons and wave indicators
- `PhaseEntry` struct with ID, Title, Status, Wave, Cost, Cycles, BlockedBy, DependsOn, StartedAt, PlanBody, Refactored
- Phase statuses: PhaseWaiting, PhaseWorking, PhaseDone, PhaseFailed, PhaseGate, PhaseSkipped
- `InitPhases(phases []PhaseInfo)` — Pre-populate from nebula manifest
- `SetPhaseStatus(phaseID string, status PhaseStatus)` — Update phase state
- `SetPhaseCost(phaseID string, cost float64)` — Track cost per phase
- `SetPhaseCycles(phaseID string, cycles int)` — Track cycles used

**`LoopView`** (`internal/tui/loopview.go`)
- Single-task cycle timeline showing coder→reviewer→coder iterations
- Displays cycle number, agent roles, costs, issues found/approved
- Spinner for in-progress indication

**`DetailPanel`** (`internal/tui/detailpanel.go`)
- Right-side expandable panel for drill-down display
- Shows plan body, agent output, diff viewer, bead hierarchy
- Supports horizontal scrolling for wide diffs

**`StatusBar`** (`internal/tui/statusbar.go`)
- Top line showing elapsed time, phase progress (done/total), cost, memory/CPU usage
- Color-codes progress based on completion ratio
- Dynamic resource indicator (yellow for high CPU/memory, red for critical)

---

## Message Architecture

**File: `internal/tui/msg.go`**

Messages are organized into three categories:

### Loop Lifecycle (Single-Task Mode)
```
MsgTaskStarted       — Task begins
MsgTaskComplete      — Task succeeds
MsgCycleStart        — Cycle N begins
MsgAgentStart        — Agent (coder/reviewer) begins
MsgAgentDone         — Agent finishes (includes cost, duration)
MsgCycleSummary      — Structured cycle metadata
MsgIssuesFound       — Reviewer found N issues
MsgApproved          — Reviewer approved code
MsgMaxCyclesReached  — Cycle limit hit
MsgBudgetExceeded    — Cost limit exceeded
MsgError/MsgInfo     — Log messages
MsgAgentOutput       — Drill-down agent output
MsgAgentDiff         — Git diff after coder work
MsgBeadUpdate        — Bead hierarchy update
```

### Phase-Contextualized (Nebula Mode)
All these messages carry a `PhaseID` so the TUI routes them to the correct per-phase LoopView:
```
MsgPhaseTaskStarted
MsgPhaseTaskComplete
MsgPhaseCycleStart
MsgPhaseAgentStart/Done
MsgPhaseAgentOutput
MsgPhaseAgentDiff
MsgPhaseCycleSummary
MsgPhaseIssuesFound
MsgPhaseApproved
MsgPhaseError/Info
MsgPhaseBeadUpdate
MsgPhaseRefactorPending   — Phase file changed mid-run
MsgPhaseRefactorApplied   — Refactor picked up by loop
MsgPhaseHotAdded          — New phase dynamically inserted
```

### Nebula Control
```
MsgNebulaInit           — Pre-populate phase table (used by NewNebulaProgram)
MsgNebulaProgress       — Worker progress callback (completed, total, openBeads, closedBeads, cost)
MsgNebulaDone           — Workers finished (results, error)
MsgGateModePrompt       — Human gate decision needed
MsgGateResolved         — User made gate decision
MsgGitPostCompletion    — Post-nebula git workflow done
MsgNebulaChoicesLoaded  — Available nebulae discovered
```

### Internal
```
MsgTick               — Elapsed-time timer tick
MsgResourceUpdate     — CPU/memory snapshot every 5s
MsgToastExpired       — Dismiss toast notification
MsgSplashDone         — Splash animation complete
```

---

## Bridge Pattern: Imperative UI → Async Messages

### UIBridge (Single-Task Mode)
**File: `internal/tui/bridge.go` - `UIBridge` struct**
- Implements `ui.UI` interface
- `program.Send()` is goroutine-safe, allows concurrent calls from multiple workers
- Each UI method call translates to a typed message
- `AgentDone()` also captures git diff after coder work via subprocess

### PhaseUIBridge (Nebula Mode)
**File: `internal/tui/bridge.go` - `PhaseUIBridge` struct**
- Identical interface but messages include `phaseID` field
- Created per-phase with `NewPhaseUIBridge(p *tea.Program, phaseID, workDir string)`
- Each nebula phase loop gets its own bridge, enabling independent cycle tracking

**Key Design: Bridge Creation**
```go
// cmd/nebula_adapters.go - tuiLoopAdapter.RunExistingPhase
phaseUI := tui.NewPhaseUIBridge(a.program, phaseID, a.workDir)
l := &loop.Loop{
  Invoker:      a.invoker,
  UI:           phaseUI,  // Per-phase bridge
  Git:          a.git,
  // ... other fields
}
result, err := l.RunExistingTask(ctx, beadID, phaseDescription)
```

---

## Nebula Parsing & Loading

### Nebula Directory Structure
```
.nebulas/<name>/
  nebula.toml           — Manifest with execution config, defaults, context, dependencies
  01-phase-name.md      — Phase 1: TOML frontmatter + markdown body
  02-phase-name.md      — Phase 2
  ...
  <name>.state.json     — Runtime state (loaded/created, not in repo)
```

### Manifest Structure (nebula.toml)
```toml
[nebula]
name = "my-nebula"
description = "What this does"

[defaults]
type = "task|bug|feature"
priority = 2
labels = ["label1"]
assignee = ""

[execution]
max_workers = 2
max_review_cycles = 5
max_budget_usd = 50.0
model = ""
gate = "trust|review|approve|watch"
agentmail = false

[context]
repo = "github.com/org/repo"
working_dir = "."
goals = ["Goal 1", "Goal 2"]
constraints = ["Constraint 1"]

[dependencies]
requires_beads = ["BID-123"]
requires_nebulae = ["other-nebula"]
```

### Phase File Format (*.md)
```markdown
+++
id = "phase-id"
title = "Human-readable title"
type = "task"
priority = 2
depends_on = ["other-phase-id"]
labels = ["label"]
assignee = ""
max_review_cycles = 5
max_budget_usd = 30.0
model = ""
gate = "review"
blocks = ["downstream-phase"]
scope = ["path/to/files/**"]
allow_scope_overlap = false
+++

## Problem

Description of what needs to change.

## Solution

How to solve it.

## Files

- `path/to/file.go` — What to modify

## Acceptance Criteria

- [ ] Criterion 1
- [ ] Criterion 2
```

### Loading Pipeline
**File: `internal/nebula/parse.go`**
```go
Load(dir string) (*Nebula, error)
  → Read nebula.toml and parse via TOML unmarshaler
  → Scan directory for *.md files
  → Parse each phase file (splitFrontmatter, unmarshal frontmatter TOML, extract body)
  → Apply defaults to zero-valued phase fields
  → Return &Nebula{Dir, Manifest, Phases}
```

**File: `internal/nebula/validate.go`**
```go
Validate(n *Nebula) []ValidationError
  → Check nebula.name is set
  → Check all phases have id and title
  → Check for duplicate IDs
  → Check dependencies reference known phase IDs
  → Check for cycles in dependency graph
  → Validate execution bounds (cycles, budget)
```

**File: `internal/nebula/types.go`**
```go
type Nebula struct {
  Dir      string
  Manifest Manifest
  Phases   []PhaseSpec
}

type PhaseSpec struct {
  ID, Title, Type, Assignee, Model string
  Priority int
  DependsOn, Labels, Blocks, Scope []string
  MaxReviewCycles int
  MaxBudgetUSD float64
  Gate GateMode
  AllowScopeOverlap bool
  Body string              // Markdown after +++
  SourceFile string        // e.g., "01-phase-name.md"
}

// Gate modes:
GateModeTrust   = "trust"     // Fully autonomous
GateModeReview  = "review"    // Pause after each phase
GateModeApprove = "approve"   // Gate plan AND each phase
GateModeWatch   = "watch"     // Stream diffs, no blocking
```

---

## Nebula Execution Flow

### Entry Point: `cmd/nebula_apply.go - runNebulaApply`

**Phase 1: Load & Validate**
```
1. Parse CLI args/flags
2. nebula.Load(dir) → full nebula parsed
3. nebula.Validate(n) → check for errors
4. nebula.LoadState(dir) → load prior state if exists
```

**Phase 2: Plan & Apply Changes to Beads**
```
5. nebula.BuildPlan(ctx, n, state, client) → determine bead create/update/close/skip actions
6. printer.NebulaPlan(plan) → display planned changes
7. nebula.Apply(ctx, plan, n, state, client) → execute plan (create/update beads in Beads CLI)
8. printer.NebulaApplyDone(plan) → confirm application
```

**Phase 3: Start Workers (--auto flag)**
```
9. If not --auto: return (end here)
10. Create nebula.WorkerGroup with max workers from flags/manifest
11. Set up either TUI or stderr path:
    
    TUI Path (if !noTUI && isStderrTTY()):
    - Create tui.NewNebulaProgram(name, phases, dir, noSplash)
    - Build tuiLoopAdapter with phase-specific UI bridges
    - wg.Runner = &tuiLoopAdapter
    - wg.Prompter = tui.NewGater(tuiProgram)
    - Launch workers in goroutine
    - Block on tuiProgram.Run()
    - On exit: check for nextNebula, repeat if set
    
    Stderr Path (default):
    - Create single shared loop.Loop
    - wg.Runner = &loopAdapter{loop}
    - Create nebula.Dashboard for live progress
    - Launch workers in goroutine with signal handler
```

**Phase 4: Post-Completion**
```
12. If git branching enabled:
    - nebula.PostCompletion(ctx, workDir, branchName)
    - Commit changes, push branch to origin, checkout main
    - Print results to user
```

### Worker Execution (Per-Phase)
**File: `cmd/nebula_adapters.go`**

```go
tuiLoopAdapter.RunExistingPhase(ctx, phaseID, beadID, title, description, exec)
  1. Create phaseUI := tui.NewPhaseUIBridge(program, phaseID, workDir)
  2. Create loop.Loop with phaseUI as UI
  3. Apply per-phase execution overrides (cycles, budget, model)
  4. loop.RunExistingTask(ctx, beadID, description)
     → Coder loop start
     → Invoke claude with description
     → phaseUI.CycleStart() → MsgPhaseCycleStart (phaseID tagged)
     → phaseUI.AgentStart("coder") → MsgPhaseAgentStart
     → phaseUI.AgentDone(...) → MsgPhaseAgentDone + git diff
     → Reviewer loop
     → ... repeat until approved or max cycles
  5. Return PhaseRunnerResult{TotalCostUSD, CyclesUsed, Report}
```

---

## Loading Animation (Splash)

**File: `internal/tui/splash.go`**

### SplashModel
```go
type SplashModel struct {
  cfg         SplashConfig
  frame       int           // Current animation frame
  totalFrames int           // Total frames before done
  done        bool          // Whether animation finished
  styleDim    lipgloss.Style
}

type SplashConfig struct {
  Width, Height  int
  OrbitRadX, OrbitRadY float64
  SpikeLen int
  FPS int
  Spins float64        // e.g., 2.0 for 2 full rotations
  ShowTitle bool
  Loop bool            // true = spinner (loops forever), false = splash (one-time)
}

DefaultSplashConfig()  // 62×19, 2 spins, no loop
SpinnerConfig()        // 36×11, 1.0 spins, loop=true
```

### Animation Timing
- **FPS**: 30 frames per second (tea.Tick every 33.3ms)
- **Spins**: 2.0 = 240 frames at 30 FPS = 8 seconds
- **Easing**: Ease-out deceleration curve `eased = 1 - (1-progress)^2.5`
- **Doppler color shift**: 9-bucket mapping based on radial velocity
- **Core rendering**: Binary stars + spikes + core + density characters

### Lifecycle
1. `NewAppModel()` creates `SplashModel` with `DefaultSplashConfig()`
2. If `--no-splash`, call `DisableSplash()` (clears splash to nil)
3. `Model.Init()` calls `Splash.Init()` if splash is not nil
4. `Splash.tick()` sends `splashTickMsg` every 33ms
5. `Splash.Update(splashTickMsg)` increments frame, checks if done
6. When `totalFrames` elapsed + 10-frame hold: `done = true`
7. On next render, splash is replaced with main view

---

## Status Report & Progress Display

### Stderr Path (Dashboard)
**File: `internal/ui/nebula.go`**

```go
NebulaProgressBar(completed, total, openBeads, closedBeads, totalCostUSD float64)
  // Format: "[nebula] 3/7 phases complete | $2.34 spent"
  // Rendered with \r to overwrite in-place (no newline)
  // Called via wg.OnProgress callback

NebulaProgressBarDone()
  // Prints final newline after progress bar

NebulaWorkerResults(results []nebula.WorkerResult)
  // Per-phase results: ✓ phase-id (bead BID-123) or ✗ phase-id — error
  // Includes ReviewReport (satisfaction, risk, human review flag, summary)

NebulaStatus(n *Nebula, state *State, m *Metrics, history []HistorySummary)
  // Comprehensive metrics: phases completed/failed, waves, cost, duration, conflicts
  // Slowest phases (top 5)
  // Historical run data
```

### TUI Path (Live Nebula View)
- `MsgNebulaProgress` callback triggered during worker execution
- Updates `NebulaView.Phases[i]` with status, cost, cycles, refactored flag
- Status bar displays real-time progress and resource usage
- Phase rows show visual indicators: icon + color + cost + cycle count

---

## Current TUI Lifecycle

1. **Startup** (`quasar nebula apply <path> --auto`)
   - Load nebula, validate, apply bead changes
   - If TTY and no --no-tui: enter TUI mode
   
2. **Splash Screen** (if not --no-splash)
   - Binary-star animation spins for ~8 seconds
   - User can press any key to skip
   
3. **Nebula View** (main screen)
   - Phase table with status, progress, cost
   - Status bar at top: elapsed time, phase progress, cost, resources
   - Detail panel on right (collapsed initially)
   - Footer shows key bindings
   
4. **Worker Execution**
   - Workers spawn per-phase loops
   - PhaseUIBridge sends phase-tagged messages
   - AppModel.Update() routes messages to per-phase LoopView
   - Detail panel drills down: phases → single phase cycles → agent output
   
5. **Gate Prompts** (if gate mode != trust)
   - MsgGatePrompt sent by gater during worker
   - Overlay appears with diff + approve/reject/skip buttons
   - User responds → Gater.Prompt() unblocks via response channel
   
6. **Completion**
   - MsgNebulaDone sent when all workers finish
   - CompletionOverlay appears with results
   - If multiple nebulae available nearby: show picker overlay
   - User selects next nebula → TUI resets and relaunches
   - On quit: TUI returns to caller
   
7. **Post-Completion (if git branching)**
   - nebula.PostCompletion() pushed branch and checkout main
   - On stderr: print results; on TUI: show in overlay
   
---

## Key Files Reference

| Component | File | Key Types/Functions |
|-----------|------|-------------------|
| **TUI Core** | `internal/tui/tui.go` | `NewProgram`, `NewNebulaProgram`, `Run` |
| **Model** | `internal/tui/model.go` | `AppModel`, `NewAppModel`, `DisableSplash` |
| **Messages** | `internal/tui/msg.go` | All `Msg*` structs |
| **Bridges** | `internal/tui/bridge.go` | `UIBridge`, `PhaseUIBridge` |
| **Nebula View** | `internal/tui/nebulaview.go` | `NebulaView`, `PhaseEntry`, `PhaseStatus` |
| **Loop View** | `internal/tui/loopview.go` | `LoopView` (per-phase timeline) |
| **Gater** | `internal/tui/gater.go` | `Gater`, `Prompt()` |
| **Splash** | `internal/tui/splash.go` | `SplashModel`, `DefaultSplashConfig` |
| **Discovery** | `internal/tui/nebula_discover.go` | `DiscoverNebulae`, `NebulaChoice` |
| **Nebula Apply** | `cmd/nebula_apply.go` | `runNebulaApply` (entry point) |
| **Adapters** | `cmd/nebula_adapters.go` | `tuiLoopAdapter`, `loopAdapter` |
| **Parsing** | `internal/nebula/parse.go` | `Load`, `parsePhaseFile` |
| **Validation** | `internal/nebula/validate.go` | `Validate` |
| **Types** | `internal/nebula/types.go` | `Nebula`, `PhaseSpec`, `Manifest`, `GateMode`, `PhaseStatus` |
| **UI Output** | `internal/ui/nebula.go` | `NebulaProgressBar`, `NebulaWorkerResults`, `NebulaStatus` |

---

## Design Patterns

### Bridge Pattern
- Imperative UI calls (e.g., `ui.TaskStarted(beadID, title)`) converted to async messages
- BubbleTea processes messages asynchronously in `Update()`
- Allows concurrent workers to update UI from multiple goroutines safely

### Phase-Tagging Pattern (Nebula Mode)
- Each message includes `PhaseID` field
- `AppModel.Update()` routes to `PhaseLoops[phaseID]`
- Enables independent per-phase cycle tracking and drill-down

### Per-Phase Loop Factory (Adapters)
- `tuiLoopAdapter.RunExistingPhase()` creates fresh `loop.Loop` per phase
- Injects phase-specific `PhaseUIBridge` as UI
- Isolates phase state and prevents cross-phase message pollution

### TUI Mode Detection
- `isStderrTTY()` checks if stderr is connected to terminal
- `--no-tui` flag forces stderr path regardless
- Allows same codebase to support both interactive (TUI) and CI/CD (plain text) usage

---

## Testing Insights

- No external test frameworks; stdlib `testing` only
- Use `tea.WithoutSignalHandler()` and `tea.WithOutput()` options for test programs
- Mock `beads.Client` and `agent.Invoker` for isolated tests
- Table-driven tests with `t.Run()` for subtests

+++
id = "facade-strategy"
title = "Build Facade over DAG engine and Strategy pattern for output modes"
type = "feature"
priority = 3
depends_on = ["impact-scoring", "union-find-tracks"]
+++

## Problem

The DAG engine has multiple subsystems (core graph, scoring, partitioning) that consumers shouldn't need to understand individually. We need a clean Facade that provides a simple API for the nebula orchestrator. Additionally, different consumers need different views of the graph (execution order, impact report, track assignment), which is a Strategy pattern opportunity.

## Solution

### Facade: TaskAnalyzer

```go
// TaskAnalyzer is the primary entry point for DAG-based task analysis.
// It wraps the core DAG, scoring, and partitioning subsystems.
type TaskAnalyzer struct {
    dag *DAG
}

func NewTaskAnalyzer() *TaskAnalyzer

// Build the graph
func (ta *TaskAnalyzer) AddTask(id string, priority int, metadata map[string]any)
func (ta *TaskAnalyzer) AddDependency(taskID, dependsOn string) error
func (ta *TaskAnalyzer) RemoveTask(id string)

// Analysis
func (ta *TaskAnalyzer) Analyze() error  // runs all scoring and partitioning
func (ta *TaskAnalyzer) ExecutionOrder() ([]string, error)
func (ta *TaskAnalyzer) ReadyTasks() []string
func (ta *TaskAnalyzer) ImpactScores() map[string]float64
func (ta *TaskAnalyzer) Tracks() []Track
func (ta *TaskAnalyzer) CriticalPath() []string

// Strategy-based output
func (ta *TaskAnalyzer) Report(strategy ReportStrategy) string
```

### Strategy: ReportStrategy

```go
// ReportStrategy defines how to present DAG analysis results.
type ReportStrategy interface {
    Render(dag *DAG, tracks []Track) string
}
```

Built-in strategies:
- `ExecutionPlanStrategy` — ordered task list with dependencies noted
- `ImpactReportStrategy` — tasks ranked by impact score with bottleneck highlights
- `TrackAssignmentStrategy` — parallel tracks with member tasks and aggregate stats
- `CriticalPathStrategy` — longest path through the DAG with timing estimates

Each robot/agent output mode is a Strategy against the shared graph state.

### Integration with Nebula

Replace or wrap the existing `internal/nebula/graph.go` usage:
- `apply.go` uses `TaskAnalyzer.ReadyTasks()` instead of manual graph traversal
- `parallelism.go` uses `TaskAnalyzer.Tracks()` for parallel scheduling
- Impact scores can influence worker allocation (high-impact phases get more review cycles)

## Files

- `internal/dag/analyzer.go` — TaskAnalyzer Facade
- `internal/dag/strategy.go` — ReportStrategy interface + built-in strategies
- `internal/dag/analyzer_test.go` — integration tests through the Facade
- `internal/dag/strategy_test.go` — tests for each strategy output

## Acceptance Criteria

- [ ] TaskAnalyzer provides a clean, simple API over all DAG subsystems
- [ ] Analyze() runs scoring and partitioning in one call
- [ ] At least 4 ReportStrategy implementations
- [ ] Each strategy produces meaningful, distinct output
- [ ] Integration path to replace nebula graph.go is documented in phase notes
- [ ] `go test ./internal/dag/...` passes

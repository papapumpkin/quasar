package nebula

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
)

// TaskRunnerResult holds the outcome of a single task execution.
type TaskRunnerResult struct {
	TotalCostUSD float64
	CyclesUsed   int
	Report       *ReviewReport
}

// TaskRunner is the interface for executing a task (satisfied by loop.Loop).
type TaskRunner interface {
	RunExistingTask(ctx context.Context, beadID, taskDescription string, exec ResolvedExecution) (*TaskRunnerResult, error)
	GenerateCheckpoint(ctx context.Context, beadID, taskDescription string) (string, error)
}

// ProgressFunc is called after each task status change to report progress.
// Parameters: completed, total, openBeads, closedBeads, totalCostUSD.
type ProgressFunc func(completed, total, openBeads, closedBeads int, totalCostUSD float64)

// WorkerGroup executes tasks in dependency order using a pool of workers.
type WorkerGroup struct {
	Runner       TaskRunner
	Nebula       *Nebula
	State        *State
	MaxWorkers   int
	Watcher      *Watcher // nil = no in-flight editing
	GlobalCycles int
	GlobalBudget float64
	GlobalModel  string
	OnProgress   ProgressFunc // optional progress callback

	mu      sync.Mutex
	results []WorkerResult
}

// buildTaskPrompt prepends nebula context (goals, constraints) to the task body.
func buildTaskPrompt(task *TaskSpec, ctx *Context) string {
	if ctx == nil || (len(ctx.Goals) == 0 && len(ctx.Constraints) == 0) {
		return task.Body
	}

	var sb strings.Builder
	sb.WriteString("PROJECT CONTEXT:\n")
	if len(ctx.Goals) > 0 {
		sb.WriteString("Goals:\n")
		for _, g := range ctx.Goals {
			sb.WriteString("- ")
			sb.WriteString(g)
			sb.WriteString("\n")
		}
	}
	if len(ctx.Constraints) > 0 {
		sb.WriteString("Constraints:\n")
		for _, c := range ctx.Constraints {
			sb.WriteString("- ")
			sb.WriteString(c)
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\nTASK:\n")
	sb.WriteString(task.Body)
	return sb.String()
}

// reportProgress calls the OnProgress callback (if set) with current counts.
// Must be called with wg.mu held.
func (wg *WorkerGroup) reportProgress() {
	if wg.OnProgress == nil {
		return
	}
	total := len(wg.Nebula.Tasks)
	var completed, open, closed int
	for _, ts := range wg.State.Tasks {
		switch ts.Status {
		case TaskStatusDone:
			closed++
			completed++
		case TaskStatusFailed:
			closed++
			completed++
		case TaskStatusInProgress, TaskStatusCreated:
			open++
		case TaskStatusPending:
			// Pending tasks have no bead yet — not counted in open or closed.
			// They still contribute to total (via len(wg.Nebula.Tasks)).
		}
	}
	wg.OnProgress(completed, total, open, closed, wg.State.TotalCostUSD)
}

// initTaskState builds lookup maps from the current nebula and state.
// It returns a task-spec index, and sets of already-done and already-failed task IDs.
// Failed tasks are also marked done so that graph.Ready() can unblock dependents.
func (wg *WorkerGroup) initTaskState() (tasksByID map[string]*TaskSpec, done, failed map[string]bool) {
	tasksByID = make(map[string]*TaskSpec)
	for i := range wg.Nebula.Tasks {
		tasksByID[wg.Nebula.Tasks[i].ID] = &wg.Nebula.Tasks[i]
	}

	done = make(map[string]bool)
	failed = make(map[string]bool)
	for id, ts := range wg.State.Tasks {
		if ts.Status == TaskStatusDone {
			done[id] = true
		}
		if ts.Status == TaskStatusFailed {
			failed[id] = true
			done[id] = true
		}
	}
	return tasksByID, done, failed
}

// filterEligible returns task IDs from ready that are not in-flight, not failed,
// and not blocked by a failed dependency.
// Must be called with wg.mu held.
func filterEligible(ready []string, inFlight, failed map[string]bool, graph *Graph) []string {
	var eligible []string
	for _, id := range ready {
		if inFlight[id] || failed[id] {
			continue
		}
		if hasFailedDep(id, failed, graph) {
			continue
		}
		eligible = append(eligible, id)
	}
	return eligible
}

// hasFailedDep reports whether any direct dependency of taskID has failed.
func hasFailedDep(taskID string, failed map[string]bool, graph *Graph) bool {
	deps, ok := graph.adjacency[taskID]
	if !ok {
		return false
	}
	for dep := range deps {
		if failed[dep] {
			return true
		}
	}
	return false
}

// executeTask runs a single task and records the result.
// It is intended to be called as a goroutine from the dispatch loop.
func (wg *WorkerGroup) executeTask(
	ctx context.Context,
	taskID string,
	tasksByID map[string]*TaskSpec,
	done, failed, inFlight map[string]bool,
) {
	task := tasksByID[taskID]
	ts := wg.State.Tasks[taskID]
	if task == nil || ts == nil || ts.BeadID == "" {
		wg.recordFailure(taskID, done, failed, inFlight)
		return
	}

	wg.mu.Lock()
	wg.State.SetTaskState(taskID, ts.BeadID, TaskStatusInProgress)
	if err := SaveState(wg.Nebula.Dir, wg.State); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save state: %v\n", err)
	}
	wg.reportProgress()
	wg.mu.Unlock()

	exec := ResolveExecution(wg.GlobalCycles, wg.GlobalBudget, wg.GlobalModel, &wg.Nebula.Manifest.Execution, task)
	prompt := buildTaskPrompt(task, &wg.Nebula.Manifest.Context)
	taskResult, err := wg.Runner.RunExistingTask(ctx, ts.BeadID, prompt, exec)

	wg.recordResult(taskID, ts, taskResult, err, done, failed, inFlight)
}

// recordResult updates state maps and persists state after a task execution.
// Must NOT be called with wg.mu held.
func (wg *WorkerGroup) recordResult(
	taskID string,
	ts *TaskState,
	taskResult *TaskRunnerResult,
	err error,
	done, failed, inFlight map[string]bool,
) {
	wg.mu.Lock()
	defer wg.mu.Unlock()

	delete(inFlight, taskID)
	wr := WorkerResult{TaskID: taskID, BeadID: ts.BeadID, Err: err}
	if taskResult != nil {
		wg.State.TotalCostUSD += taskResult.TotalCostUSD
	}
	if err == nil && taskResult != nil && taskResult.Report != nil {
		wr.Report = taskResult.Report
		ts.Report = taskResult.Report
	}
	wg.results = append(wg.results, wr)

	if err != nil {
		failed[taskID] = true
		done[taskID] = true // unblock dependents (blocked-by-failure filter skips them)
		wg.State.SetTaskState(taskID, ts.BeadID, TaskStatusFailed)
	} else {
		done[taskID] = true
		wg.State.SetTaskState(taskID, ts.BeadID, TaskStatusDone)
	}
	if err := SaveState(wg.Nebula.Dir, wg.State); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save state: %v\n", err)
	}
	wg.reportProgress()
}

// recordFailure marks a task as failed when it has no valid bead ID.
// Must NOT be called with wg.mu held.
func (wg *WorkerGroup) recordFailure(taskID string, done, failed, inFlight map[string]bool) {
	wg.mu.Lock()
	failed[taskID] = true
	done[taskID] = true
	delete(inFlight, taskID)
	wg.results = append(wg.results, WorkerResult{
		TaskID: taskID,
		Err:    fmt.Errorf("no bead ID for task %q", taskID),
	})
	wg.mu.Unlock()
}

// Run dispatches tasks respecting dependency order.
// It returns after all eligible tasks have been executed or the context is canceled.
func (wg *WorkerGroup) Run(ctx context.Context) ([]WorkerResult, error) {
	if wg.MaxWorkers <= 0 {
		wg.MaxWorkers = 1
	}
	tasksByID, done, failed := wg.initTaskState()
	graph := NewGraph(wg.Nebula.Tasks)
	inFlight := make(map[string]bool)
	sem := make(chan struct{}, wg.MaxWorkers)
	var wgSync sync.WaitGroup

	for ctx.Err() == nil {
		wg.mu.Lock()
		eligible := filterEligible(graph.Ready(done), inFlight, failed, graph)
		anyInFlight := len(inFlight) > 0
		wg.mu.Unlock()

		if len(eligible) == 0 {
			if !anyInFlight {
				break
			}
			wgSync.Wait()
			continue
		}

		for _, id := range eligible {
			if ctx.Err() != nil {
				break
			}
			wg.mu.Lock()
			inFlight[id] = true
			wg.mu.Unlock()

			sem <- struct{}{}
			wgSync.Add(1)
			go func(taskID string) {
				defer func() { <-sem; wgSync.Done() }()
				wg.executeTask(ctx, taskID, tasksByID, done, failed, inFlight)
			}(id)
		}
		wgSync.Wait() // wait for batch before looking for more ready tasks
	}
	wgSync.Wait()

	wg.mu.Lock()
	results := wg.results
	wg.mu.Unlock()
	return results, nil
}

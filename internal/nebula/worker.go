package nebula

import (
	"context"
	"fmt"
	"sync"
)

// TaskRunner is the interface for executing a task (satisfied by loop.Loop).
type TaskRunner interface {
	RunExistingTask(ctx context.Context, beadID, taskDescription string) error
}

// WorkerGroup executes tasks in dependency order using a pool of workers.
type WorkerGroup struct {
	Runner     TaskRunner
	Nebula     *Nebula
	State      *State
	MaxWorkers int

	mu      sync.Mutex
	results []WorkerResult
}

// Run dispatches tasks respecting dependency order.
// It returns after all eligible tasks have been executed or the context is cancelled.
func (wg *WorkerGroup) Run(ctx context.Context) ([]WorkerResult, error) {
	if wg.MaxWorkers <= 0 {
		wg.MaxWorkers = 1
	}

	tasksByID := make(map[string]*TaskSpec)
	for i := range wg.Nebula.Tasks {
		tasksByID[wg.Nebula.Tasks[i].ID] = &wg.Nebula.Tasks[i]
	}

	graph := NewGraph(wg.Nebula.Tasks)

	// Build initial done set from state.
	done := make(map[string]bool)
	failed := make(map[string]bool)
	for id, ts := range wg.State.Tasks {
		if ts.Status == TaskStatusDone {
			done[id] = true
		}
		if ts.Status == TaskStatusFailed {
			failed[id] = true
		}
	}

	// inFlight tracks currently executing task IDs.
	inFlight := make(map[string]bool)

	sem := make(chan struct{}, wg.MaxWorkers)
	var wgSync sync.WaitGroup

	for {
		if ctx.Err() != nil {
			break
		}

		wg.mu.Lock()
		ready := graph.Ready(done)

		// Filter out in-flight, failed, and tasks blocked by failures.
		var eligible []string
		for _, id := range ready {
			if inFlight[id] || failed[id] {
				continue
			}
			// Check if any dependency has failed.
			blockedByFailure := false
			if deps, ok := graph.adjacency[id]; ok {
				for dep := range deps {
					if failed[dep] {
						blockedByFailure = true
						break
					}
				}
			}
			if blockedByFailure {
				continue
			}
			eligible = append(eligible, id)
		}
		wg.mu.Unlock()

		if len(eligible) == 0 {
			// Check if anything is still in flight.
			wg.mu.Lock()
			anyInFlight := len(inFlight) > 0
			wg.mu.Unlock()
			if !anyInFlight {
				break
			}
			// Wait for an in-flight task to finish.
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
				defer func() {
					<-sem
					wgSync.Done()
				}()

				task := tasksByID[taskID]
				ts := wg.State.Tasks[taskID]
				if task == nil || ts == nil || ts.BeadID == "" {
					wg.mu.Lock()
					failed[taskID] = true
					delete(inFlight, taskID)
					wg.results = append(wg.results, WorkerResult{
						TaskID: taskID,
						Err:    fmt.Errorf("no bead ID for task %q", taskID),
					})
					wg.mu.Unlock()
					return
				}

				err := wg.Runner.RunExistingTask(ctx, ts.BeadID, task.Body)

				wg.mu.Lock()
				delete(inFlight, taskID)
				result := WorkerResult{TaskID: taskID, BeadID: ts.BeadID, Err: err}
				wg.results = append(wg.results, result)

				if err != nil {
					failed[taskID] = true
					wg.State.SetTaskState(taskID, ts.BeadID, TaskStatusFailed)
				} else {
					done[taskID] = true
					wg.State.SetTaskState(taskID, ts.BeadID, TaskStatusDone)
				}
				_ = SaveState(wg.Nebula.Dir, wg.State)
				wg.mu.Unlock()
			}(id)
		}

		// Wait for this batch to complete before looking for more ready tasks.
		wgSync.Wait()
	}

	wgSync.Wait()

	wg.mu.Lock()
	results := wg.results
	wg.mu.Unlock()

	return results, nil
}

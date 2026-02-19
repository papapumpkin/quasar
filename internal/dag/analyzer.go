// Package dag provides a directed acyclic graph engine for modeling task
// dependencies, impact scoring, and parallel track partitioning.

package dag

// TaskAnalyzer is the primary entry point for DAG-based task analysis.
// It wraps the core DAG, scoring, and partitioning subsystems behind a
// single coherent API. Consumers build the graph via AddTask/AddDependency,
// then call Analyze to compute all derived metrics at once.
type TaskAnalyzer struct {
	dag    *DAG
	tracks []Track
	scored bool
}

// NewTaskAnalyzer creates a TaskAnalyzer backed by a fresh, empty DAG.
func NewTaskAnalyzer() *TaskAnalyzer {
	return &TaskAnalyzer{
		dag: New(),
	}
}

// AddTask adds a task node with the given ID, priority, and optional
// metadata. If metadata is nil, an empty map is used. Returns an error
// if a task with the same ID already exists.
func (ta *TaskAnalyzer) AddTask(id string, priority int, metadata map[string]any) error {
	if err := ta.dag.AddNode(id, priority); err != nil {
		return err
	}
	if metadata != nil {
		node := ta.dag.Node(id)
		node.Metadata = metadata
	}
	ta.invalidate()
	return nil
}

// AddDependency declares that taskID depends on dependsOn. Both tasks
// must already have been added via AddTask. Returns an error if either
// task is missing, the edge would be a self-loop, or it would create a
// cycle.
func (ta *TaskAnalyzer) AddDependency(taskID, dependsOn string) error {
	if err := ta.dag.AddEdge(taskID, dependsOn); err != nil {
		return err
	}
	ta.invalidate()
	return nil
}

// RemoveTask removes a task and all its associated dependency edges.
// Returns an error if the task does not exist.
func (ta *TaskAnalyzer) RemoveTask(id string) error {
	if err := ta.dag.Remove(id); err != nil {
		return err
	}
	ta.invalidate()
	return nil
}

// Analyze runs all scoring and partitioning passes in one call:
// composite impact scoring (PageRank + Betweenness) followed by
// Union-Find track partitioning. After Analyze returns successfully,
// ImpactScores, Tracks, and CriticalPath reflect current graph state.
func (ta *TaskAnalyzer) Analyze() error {
	if err := ta.dag.ComputeImpact(DefaultScoringOptions()); err != nil {
		return err
	}
	tracks, err := ta.dag.ComputeTracks()
	if err != nil {
		return err
	}
	ta.tracks = tracks
	ta.scored = true
	return nil
}

// ExecutionOrder returns task IDs in topological order with priority
// tie-breaking. Dependencies appear before their dependents. Returns
// ErrCycle if the graph contains a cycle.
func (ta *TaskAnalyzer) ExecutionOrder() ([]string, error) {
	return ta.dag.TopologicalSort()
}

// ReadyTasks returns task IDs that have no incomplete dependencies,
// given no tasks are yet completed. For incremental readiness, call
// ReadyWithDone.
func (ta *TaskAnalyzer) ReadyTasks() []string {
	return ta.dag.Ready(nil)
}

// ReadyWithDone returns task IDs whose dependencies are all in the
// done set, sorted by priority descending.
func (ta *TaskAnalyzer) ReadyWithDone(done map[string]bool) []string {
	return ta.dag.Ready(done)
}

// ImpactScores returns the composite impact score for every task.
// Call Analyze first; returns nil if scoring has not been run.
func (ta *TaskAnalyzer) ImpactScores() map[string]float64 {
	if !ta.scored {
		return nil
	}
	scores := make(map[string]float64, ta.dag.Len())
	for _, id := range ta.dag.Nodes() {
		scores[id] = ta.dag.Node(id).Impact
	}
	return scores
}

// Tracks returns the independent parallel tracks computed by Analyze.
// Each track can be assigned to a separate agent. Returns nil if
// Analyze has not been called.
func (ta *TaskAnalyzer) Tracks() []Track {
	return ta.tracks
}

// CriticalPath returns the longest path through the DAG measured by
// number of nodes. This identifies the sequence of tasks that
// determines the minimum total execution time when parallelism is
// unlimited. Returns an error if the graph contains a cycle.
func (ta *TaskAnalyzer) CriticalPath() ([]string, error) {
	order, err := ta.dag.TopologicalSort()
	if err != nil {
		return nil, err
	}
	if len(order) == 0 {
		return nil, nil
	}
	return computeCriticalPath(ta.dag, order), nil
}

// DAG returns the underlying DAG for advanced queries or direct access
// by strategies. This exposes the full DAG API for consumers that need
// more than the Facade provides.
func (ta *TaskAnalyzer) DAG() *DAG {
	return ta.dag
}

// Len returns the number of tasks in the analyzer.
func (ta *TaskAnalyzer) Len() int {
	return ta.dag.Len()
}

// Report generates a string report of the analysis using the given
// strategy. Different strategies produce different views of the same
// underlying graph data.
func (ta *TaskAnalyzer) Report(strategy ReportStrategy) string {
	return strategy.Render(ta.dag, ta.tracks)
}

// invalidate clears cached analysis results so they are recomputed
// on the next Analyze call.
func (ta *TaskAnalyzer) invalidate() {
	ta.tracks = nil
	ta.scored = false
}

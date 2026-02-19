package dag

import (
	"strings"
	"testing"
)

// buildAnalyzerDiamond creates a diamond-shaped task graph:
//
//	A → B → D
//	A → C → D
//
// A depends on B and C; B and C depend on D.
func buildAnalyzerDiamond(t *testing.T) *TaskAnalyzer {
	t.Helper()
	ta := NewTaskAnalyzer()
	for _, task := range []struct {
		id       string
		priority int
	}{
		{"D", 1},
		{"B", 2},
		{"C", 3},
		{"A", 4},
	} {
		if err := ta.AddTask(task.id, task.priority, nil); err != nil {
			t.Fatalf("AddTask(%q): %v", task.id, err)
		}
	}
	for _, dep := range [][2]string{
		{"A", "B"}, {"A", "C"}, {"B", "D"}, {"C", "D"},
	} {
		if err := ta.AddDependency(dep[0], dep[1]); err != nil {
			t.Fatalf("AddDependency(%q, %q): %v", dep[0], dep[1], err)
		}
	}
	return ta
}

// buildAnalyzerParallel creates two independent chains:
//
//	A → B (chain 1)
//	C → D (chain 2)
func buildAnalyzerParallel(t *testing.T) *TaskAnalyzer {
	t.Helper()
	ta := NewTaskAnalyzer()
	for _, task := range []struct {
		id       string
		priority int
	}{
		{"A", 2}, {"B", 1}, {"C", 2}, {"D", 1},
	} {
		if err := ta.AddTask(task.id, task.priority, nil); err != nil {
			t.Fatalf("AddTask(%q): %v", task.id, err)
		}
	}
	if err := ta.AddDependency("A", "B"); err != nil {
		t.Fatal(err)
	}
	if err := ta.AddDependency("C", "D"); err != nil {
		t.Fatal(err)
	}
	return ta
}

func TestNewTaskAnalyzer(t *testing.T) {
	t.Parallel()
	ta := NewTaskAnalyzer()
	if ta.Len() != 0 {
		t.Errorf("new analyzer Len() = %d, want 0", ta.Len())
	}
	if scores := ta.ImpactScores(); scores != nil {
		t.Errorf("ImpactScores before Analyze() = %v, want nil", scores)
	}
	if tracks := ta.Tracks(); tracks != nil {
		t.Errorf("Tracks before Analyze() = %v, want nil", tracks)
	}
}

func TestAddTask(t *testing.T) {
	t.Parallel()
	ta := NewTaskAnalyzer()

	t.Run("basic", func(t *testing.T) {
		err := ta.AddTask("task-1", 5, map[string]any{"label": "test"})
		if err != nil {
			t.Fatalf("AddTask: %v", err)
		}
		if ta.Len() != 1 {
			t.Errorf("Len() = %d, want 1", ta.Len())
		}
		node := ta.DAG().Node("task-1")
		if node == nil {
			t.Fatal("node not found")
		}
		if node.Priority != 5 {
			t.Errorf("Priority = %d, want 5", node.Priority)
		}
		if node.Metadata["label"] != "test" {
			t.Errorf("Metadata[label] = %v, want test", node.Metadata["label"])
		}
	})

	t.Run("duplicate", func(t *testing.T) {
		err := ta.AddTask("task-1", 1, nil)
		if err == nil {
			t.Error("expected error for duplicate task")
		}
	})

	t.Run("nil metadata", func(t *testing.T) {
		err := ta.AddTask("task-2", 1, nil)
		if err != nil {
			t.Fatalf("AddTask: %v", err)
		}
		node := ta.DAG().Node("task-2")
		if node.Metadata == nil {
			t.Error("Metadata should be initialized even when nil is passed")
		}
	})
}

func TestAddDependency(t *testing.T) {
	t.Parallel()
	ta := NewTaskAnalyzer()
	if err := ta.AddTask("A", 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := ta.AddTask("B", 1, nil); err != nil {
		t.Fatal(err)
	}

	t.Run("valid", func(t *testing.T) {
		if err := ta.AddDependency("A", "B"); err != nil {
			t.Fatalf("AddDependency: %v", err)
		}
	})

	t.Run("missing node", func(t *testing.T) {
		err := ta.AddDependency("A", "Z")
		if err == nil {
			t.Error("expected error for missing node")
		}
	})

	t.Run("cycle", func(t *testing.T) {
		err := ta.AddDependency("B", "A")
		if err == nil {
			t.Error("expected error for cycle")
		}
	})
}

func TestRemoveTask(t *testing.T) {
	t.Parallel()
	ta := buildAnalyzerDiamond(t)
	if err := ta.RemoveTask("B"); err != nil {
		t.Fatalf("RemoveTask: %v", err)
	}
	if ta.Len() != 3 {
		t.Errorf("Len() = %d, want 3", ta.Len())
	}
	if ta.DAG().Node("B") != nil {
		t.Error("node B should be removed")
	}
}

func TestRemoveTaskNotFound(t *testing.T) {
	t.Parallel()
	ta := NewTaskAnalyzer()
	err := ta.RemoveTask("missing")
	if err == nil {
		t.Error("expected error for missing task")
	}
}

func TestAnalyze(t *testing.T) {
	t.Parallel()
	ta := buildAnalyzerDiamond(t)
	if err := ta.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	// Impact scores should be populated.
	scores := ta.ImpactScores()
	if scores == nil {
		t.Fatal("ImpactScores returned nil after Analyze")
	}
	if len(scores) != 4 {
		t.Errorf("ImpactScores has %d entries, want 4", len(scores))
	}

	// D is the root dependency; should have high impact.
	if scores["D"] <= 0 {
		t.Errorf("D impact = %f, want > 0", scores["D"])
	}

	// Tracks should be computed.
	tracks := ta.Tracks()
	if tracks == nil {
		t.Fatal("Tracks returned nil after Analyze")
	}
	// Diamond is a single connected component, so one track.
	if len(tracks) != 1 {
		t.Errorf("got %d tracks, want 1 for diamond graph", len(tracks))
	}
}

func TestAnalyzeEmpty(t *testing.T) {
	t.Parallel()
	ta := NewTaskAnalyzer()
	if err := ta.Analyze(); err != nil {
		t.Fatalf("Analyze on empty: %v", err)
	}
}

func TestAnalyzeInvalidatesOnMutation(t *testing.T) {
	t.Parallel()
	ta := buildAnalyzerDiamond(t)
	if err := ta.Analyze(); err != nil {
		t.Fatal(err)
	}
	if ta.ImpactScores() == nil {
		t.Fatal("scores should be set after Analyze")
	}

	// Adding a task invalidates cached results.
	if err := ta.AddTask("E", 1, nil); err != nil {
		t.Fatal(err)
	}
	if ta.ImpactScores() != nil {
		t.Error("ImpactScores should be nil after mutation")
	}
	if ta.Tracks() != nil {
		t.Error("Tracks should be nil after mutation")
	}
}

func TestExecutionOrder(t *testing.T) {
	t.Parallel()
	ta := buildAnalyzerDiamond(t)
	order, err := ta.ExecutionOrder()
	if err != nil {
		t.Fatalf("ExecutionOrder: %v", err)
	}
	if len(order) != 4 {
		t.Fatalf("got %d nodes, want 4", len(order))
	}

	// D must appear before B and C; B and C before A.
	pos := make(map[string]int, len(order))
	for i, id := range order {
		pos[id] = i
	}
	if pos["D"] >= pos["B"] || pos["D"] >= pos["C"] {
		t.Errorf("D should appear before B and C: %v", order)
	}
	if pos["B"] >= pos["A"] || pos["C"] >= pos["A"] {
		t.Errorf("B and C should appear before A: %v", order)
	}
}

func TestReadyTasks(t *testing.T) {
	t.Parallel()
	ta := buildAnalyzerDiamond(t)

	t.Run("initial", func(t *testing.T) {
		ready := ta.ReadyTasks()
		// Only D has no dependencies.
		if len(ready) != 1 || ready[0] != "D" {
			t.Errorf("ReadyTasks() = %v, want [D]", ready)
		}
	})

	t.Run("with done", func(t *testing.T) {
		done := map[string]bool{"D": true}
		ready := ta.ReadyWithDone(done)
		// B and C should be ready once D is done.
		if len(ready) != 2 {
			t.Errorf("ReadyWithDone() = %v, want 2 tasks", ready)
		}
	})
}

func TestCriticalPath(t *testing.T) {
	t.Parallel()

	t.Run("diamond", func(t *testing.T) {
		t.Parallel()
		ta := buildAnalyzerDiamond(t)
		path, err := ta.CriticalPath()
		if err != nil {
			t.Fatalf("CriticalPath: %v", err)
		}
		// Diamond has two paths of length 3: D→B→A and D→C→A.
		if len(path) != 3 {
			t.Errorf("critical path length = %d, want 3: %v", len(path), path)
		}
		// Path must start with D and end with A.
		if path[0] != "D" {
			t.Errorf("path[0] = %q, want D", path[0])
		}
		if path[len(path)-1] != "A" {
			t.Errorf("path[-1] = %q, want A", path[len(path)-1])
		}
	})

	t.Run("chain", func(t *testing.T) {
		t.Parallel()
		ta := NewTaskAnalyzer()
		for _, id := range []string{"A", "B", "C"} {
			if err := ta.AddTask(id, 0, nil); err != nil {
				t.Fatal(err)
			}
		}
		if err := ta.AddDependency("A", "B"); err != nil {
			t.Fatal(err)
		}
		if err := ta.AddDependency("B", "C"); err != nil {
			t.Fatal(err)
		}
		path, err := ta.CriticalPath()
		if err != nil {
			t.Fatal(err)
		}
		if len(path) != 3 {
			t.Fatalf("critical path length = %d, want 3", len(path))
		}
		expected := []string{"C", "B", "A"}
		for i, id := range expected {
			if path[i] != id {
				t.Errorf("path[%d] = %q, want %q", i, path[i], id)
			}
		}
	})

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		ta := NewTaskAnalyzer()
		path, err := ta.CriticalPath()
		if err != nil {
			t.Fatal(err)
		}
		if path != nil {
			t.Errorf("CriticalPath on empty = %v, want nil", path)
		}
	})
}

func TestParallelTracks(t *testing.T) {
	t.Parallel()
	ta := buildAnalyzerParallel(t)
	if err := ta.Analyze(); err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	tracks := ta.Tracks()
	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2", len(tracks))
	}

	// Each track should have 2 nodes.
	for _, tr := range tracks {
		if len(tr.NodeIDs) != 2 {
			t.Errorf("track %d has %d nodes, want 2", tr.ID, len(tr.NodeIDs))
		}
	}
}

func TestReport(t *testing.T) {
	t.Parallel()
	ta := buildAnalyzerDiamond(t)
	if err := ta.Analyze(); err != nil {
		t.Fatal(err)
	}

	strategies := []struct {
		name     string
		strategy ReportStrategy
		contains string
	}{
		{"ExecutionPlan", ExecutionPlanStrategy{}, "Execution Plan"},
		{"ImpactReport", ImpactReportStrategy{}, "Impact Report"},
		{"TrackAssignment", TrackAssignmentStrategy{}, "Track Assignments"},
		{"CriticalPath", CriticalPathStrategy{}, "Critical Path"},
	}

	for _, tc := range strategies {
		t.Run(tc.name, func(t *testing.T) {
			report := ta.Report(tc.strategy)
			if !strings.Contains(report, tc.contains) {
				t.Errorf("report missing %q:\n%s", tc.contains, report)
			}
			if report == "" {
				t.Error("report should not be empty")
			}
		})
	}
}

func TestDAGAccessor(t *testing.T) {
	t.Parallel()
	ta := NewTaskAnalyzer()
	if ta.DAG() == nil {
		t.Error("DAG() should not be nil")
	}
}

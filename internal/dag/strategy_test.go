package dag

import (
	"strings"
	"testing"
)

// buildStrategyFixture creates a well-characterized graph for strategy testing:
//
//	A (p=4) → B (p=2) → D (p=1)
//	A (p=4) → C (p=3) → D (p=1)
//	E (p=1) — independent node
//
// After Analyze: D has highest impact (root dependency), E is in its own track.
func buildStrategyFixture(t *testing.T) *TaskAnalyzer {
	t.Helper()
	ta := NewTaskAnalyzer()
	for _, task := range []struct {
		id       string
		priority int
	}{
		{"D", 1}, {"B", 2}, {"C", 3}, {"A", 4}, {"E", 1},
	} {
		if err := ta.AddTask(task.id, task.priority, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, dep := range [][2]string{
		{"A", "B"}, {"A", "C"}, {"B", "D"}, {"C", "D"},
	} {
		if err := ta.AddDependency(dep[0], dep[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := ta.Analyze(); err != nil {
		t.Fatal(err)
	}
	return ta
}

func TestExecutionPlanStrategy(t *testing.T) {
	t.Parallel()
	ta := buildStrategyFixture(t)
	strategy := ExecutionPlanStrategy{}
	report := ta.Report(strategy)

	t.Run("header", func(t *testing.T) {
		if !strings.Contains(report, "# Execution Plan") {
			t.Error("missing header")
		}
	})

	t.Run("numbered steps", func(t *testing.T) {
		if !strings.Contains(report, "1.") {
			t.Error("missing numbered steps")
		}
	})

	t.Run("dependencies shown", func(t *testing.T) {
		if !strings.Contains(report, "depends on:") {
			t.Error("dependencies not shown")
		}
	})

	t.Run("all nodes present", func(t *testing.T) {
		for _, id := range []string{"A", "B", "C", "D", "E"} {
			if !strings.Contains(report, id) {
				t.Errorf("missing node %q in report", id)
			}
		}
	})

	t.Run("topological order", func(t *testing.T) {
		// D must appear before B in the output.
		dPos := strings.Index(report, " D ")
		bPos := strings.Index(report, " B ")
		if dPos >= bPos {
			t.Error("D should appear before B in execution plan")
		}
	})
}

func TestExecutionPlanStrategyEmpty(t *testing.T) {
	t.Parallel()
	ta := NewTaskAnalyzer()
	report := ta.Report(ExecutionPlanStrategy{})
	if !strings.Contains(report, "No tasks") {
		t.Errorf("empty graph should say 'No tasks': %q", report)
	}
}

func TestImpactReportStrategy(t *testing.T) {
	t.Parallel()
	ta := buildStrategyFixture(t)
	strategy := ImpactReportStrategy{}
	report := ta.Report(strategy)

	t.Run("header", func(t *testing.T) {
		if !strings.Contains(report, "# Impact Report") {
			t.Error("missing header")
		}
	})

	t.Run("impact scores shown", func(t *testing.T) {
		if !strings.Contains(report, "impact=") {
			t.Error("impact scores not shown")
		}
	})

	t.Run("bottleneck highlighted", func(t *testing.T) {
		if !strings.Contains(report, "BOTTLENECK") {
			t.Error("no bottleneck highlights in report")
		}
	})

	t.Run("ranked order", func(t *testing.T) {
		// First ranked task should be the highest impact.
		lines := strings.Split(report, "\n")
		var firstRanked string
		for _, line := range lines {
			if strings.HasPrefix(line, "1.") {
				firstRanked = line
				break
			}
		}
		if firstRanked == "" {
			t.Fatal("no ranked entries found")
		}
		// D should be highest impact in the diamond (most depended upon).
		if !strings.Contains(firstRanked, "D") {
			t.Errorf("expected D as highest impact, got: %s", firstRanked)
		}
	})
}

func TestImpactReportStrategyEmpty(t *testing.T) {
	t.Parallel()
	ta := NewTaskAnalyzer()
	report := ta.Report(ImpactReportStrategy{})
	if !strings.Contains(report, "No tasks") {
		t.Errorf("empty graph should say 'No tasks': %q", report)
	}
}

func TestTrackAssignmentStrategy(t *testing.T) {
	t.Parallel()
	ta := buildStrategyFixture(t)
	strategy := TrackAssignmentStrategy{}
	report := ta.Report(strategy)

	t.Run("header", func(t *testing.T) {
		if !strings.Contains(report, "# Track Assignments") {
			t.Error("missing header")
		}
	})

	t.Run("total tracks", func(t *testing.T) {
		// Diamond + independent E = 2 tracks.
		if !strings.Contains(report, "Total tracks: 2") {
			t.Errorf("expected 2 tracks: %s", report)
		}
	})

	t.Run("track details", func(t *testing.T) {
		if !strings.Contains(report, "## Track") {
			t.Error("missing track section headers")
		}
	})

	t.Run("node details", func(t *testing.T) {
		if !strings.Contains(report, "priority:") {
			t.Error("missing node priority details")
		}
		if !strings.Contains(report, "impact:") {
			t.Error("missing node impact details")
		}
	})

	t.Run("all nodes present", func(t *testing.T) {
		for _, id := range []string{"A", "B", "C", "D", "E"} {
			if !strings.Contains(report, id) {
				t.Errorf("missing node %q", id)
			}
		}
	})
}

func TestTrackAssignmentStrategyNoTracks(t *testing.T) {
	t.Parallel()
	ta := NewTaskAnalyzer()
	report := ta.Report(TrackAssignmentStrategy{})
	if !strings.Contains(report, "No tracks") {
		t.Errorf("should say no tracks: %q", report)
	}
}

func TestCriticalPathStrategy(t *testing.T) {
	t.Parallel()
	ta := buildStrategyFixture(t)
	strategy := CriticalPathStrategy{}
	report := ta.Report(strategy)

	t.Run("header", func(t *testing.T) {
		if !strings.Contains(report, "# Critical Path") {
			t.Error("missing header")
		}
	})

	t.Run("length info", func(t *testing.T) {
		if !strings.Contains(report, "Length:") {
			t.Error("missing length information")
		}
	})

	t.Run("percentage", func(t *testing.T) {
		// Path should be 3 of 5 = 60%.
		if !strings.Contains(report, "60%") {
			t.Errorf("expected 60%% in report: %s", report)
		}
	})

	t.Run("arrows", func(t *testing.T) {
		if !strings.Contains(report, "→") {
			t.Error("missing arrow indicators between steps")
		}
	})

	t.Run("advice", func(t *testing.T) {
		if !strings.Contains(report, "parallelizing") {
			t.Error("missing optimization advice")
		}
	})

	t.Run("path endpoints", func(t *testing.T) {
		// Path should start with D and end with A.
		lines := strings.Split(report, "\n")
		var firstStep, lastStep string
		for _, line := range lines {
			if strings.HasPrefix(line, "1.") {
				firstStep = line
			}
			// Find last numbered line with a node.
			if strings.HasPrefix(line, "3.") {
				lastStep = line
			}
		}
		if !strings.Contains(firstStep, "D") {
			t.Errorf("path should start with D: %s", firstStep)
		}
		if !strings.Contains(lastStep, "A") {
			t.Errorf("path should end with A: %s", lastStep)
		}
	})
}

func TestCriticalPathStrategyEmpty(t *testing.T) {
	t.Parallel()
	ta := NewTaskAnalyzer()
	report := ta.Report(CriticalPathStrategy{})
	if !strings.Contains(report, "No tasks") {
		t.Errorf("empty graph should say 'No tasks': %q", report)
	}
}

// TestStrategyDistinctOutput verifies that each strategy produces
// meaningfully different output for the same graph.
func TestStrategyDistinctOutput(t *testing.T) {
	t.Parallel()
	ta := buildStrategyFixture(t)
	strategies := []ReportStrategy{
		ExecutionPlanStrategy{},
		ImpactReportStrategy{},
		TrackAssignmentStrategy{},
		CriticalPathStrategy{},
	}

	reports := make([]string, len(strategies))
	for i, s := range strategies {
		reports[i] = ta.Report(s)
	}

	// Each pair of reports should be different.
	for i := 0; i < len(reports); i++ {
		for j := i + 1; j < len(reports); j++ {
			if reports[i] == reports[j] {
				t.Errorf("strategy %d and %d produced identical output", i, j)
			}
		}
	}
}

// TestStrategyInterfaceCompliance verifies all built-in strategies
// satisfy the ReportStrategy interface at compile time.
func TestStrategyInterfaceCompliance(t *testing.T) {
	t.Parallel()
	var _ ReportStrategy = ExecutionPlanStrategy{}
	var _ ReportStrategy = ImpactReportStrategy{}
	var _ ReportStrategy = TrackAssignmentStrategy{}
	var _ ReportStrategy = CriticalPathStrategy{}
}

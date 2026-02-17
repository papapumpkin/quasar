package loop

import (
	"strings"
	"testing"
)

func TestDrainRefactor_NilChannel(t *testing.T) {
	t.Parallel()
	l := &Loop{}
	state := &CycleState{TaskTitle: "original"}
	l.drainRefactor(state)
	if state.TaskTitle != "original" {
		t.Errorf("TaskTitle changed to %q, want %q", state.TaskTitle, "original")
	}
	if state.Refactored {
		t.Error("Refactored set to true, want false")
	}
}

func TestDrainRefactor_EmptyChannel(t *testing.T) {
	t.Parallel()
	ch := make(chan string, 1)
	l := &Loop{RefactorCh: ch}
	state := &CycleState{TaskTitle: "original"}
	l.drainRefactor(state)
	if state.TaskTitle != "original" {
		t.Errorf("TaskTitle changed to %q, want %q", state.TaskTitle, "original")
	}
	if state.Refactored {
		t.Error("Refactored set to true, want false")
	}
}

func TestDrainRefactor_SingleValue(t *testing.T) {
	t.Parallel()
	ch := make(chan string, 1)
	ch <- "updated body"
	l := &Loop{RefactorCh: ch}
	state := &CycleState{TaskTitle: "original"}
	l.drainRefactor(state)
	if state.TaskTitle != "updated body" {
		t.Errorf("TaskTitle = %q, want %q", state.TaskTitle, "updated body")
	}
	if state.OriginalDescription != "original" {
		t.Errorf("OriginalDescription = %q, want %q", state.OriginalDescription, "original")
	}
	if state.RefactorDescription != "updated body" {
		t.Errorf("RefactorDescription = %q, want %q", state.RefactorDescription, "updated body")
	}
	if !state.Refactored {
		t.Error("Refactored = false, want true")
	}
}

func TestDrainRefactor_MultipleValues(t *testing.T) {
	t.Parallel()
	ch := make(chan string, 3)
	ch <- "first"
	ch <- "second"
	ch <- "third"
	l := &Loop{RefactorCh: ch}
	state := &CycleState{TaskTitle: "original"}
	l.drainRefactor(state)
	if state.TaskTitle != "third" {
		t.Errorf("TaskTitle = %q, want %q (last value wins)", state.TaskTitle, "third")
	}
	if state.OriginalDescription != "original" {
		t.Errorf("OriginalDescription = %q, want %q", state.OriginalDescription, "original")
	}
	if !state.Refactored {
		t.Error("Refactored = false, want true")
	}
}

func TestDrainRefactor_PreservesOriginal(t *testing.T) {
	t.Parallel()
	ch := make(chan string, 1)
	ch <- "v2"
	l := &Loop{RefactorCh: ch}
	state := &CycleState{
		TaskTitle:           "v1",
		OriginalDescription: "v0",
		Refactored:          true,
	}
	l.drainRefactor(state)
	// OriginalDescription should be overwritten with current TaskTitle ("v1")
	// because a new refactor replaces the snapshot.
	if state.OriginalDescription != "v1" {
		t.Errorf("OriginalDescription = %q, want %q", state.OriginalDescription, "v1")
	}
	if state.TaskTitle != "v2" {
		t.Errorf("TaskTitle = %q, want %q", state.TaskTitle, "v2")
	}
}

func TestBuildCoderPrompt_Refactored(t *testing.T) {
	t.Parallel()
	l := &Loop{}
	state := &CycleState{
		TaskBeadID:          "bead-123",
		TaskTitle:           "updated task",
		Cycle:               2,
		Refactored:          true,
		OriginalDescription: "original task",
		RefactorDescription: "updated task",
		CoderOutput:         "I made some changes to file.go",
		Findings: []ReviewFinding{
			{Severity: "major", Description: "missing error check"},
		},
	}

	prompt := l.buildCoderPrompt(state)

	// Prompt must contain the REFACTOR section.
	if !strings.Contains(prompt, "[REFACTOR — USER UPDATE]") {
		t.Error("prompt missing [REFACTOR — USER UPDATE] section")
	}
	// Must contain original and updated descriptions.
	if !strings.Contains(prompt, "original task") {
		t.Error("prompt missing original description")
	}
	if !strings.Contains(prompt, "updated task") {
		t.Error("prompt missing updated description")
	}
	// Must contain previous work context.
	if !strings.Contains(prompt, "[PREVIOUS WORK]") {
		t.Error("prompt missing [PREVIOUS WORK] section")
	}
	if !strings.Contains(prompt, "I made some changes") {
		t.Error("prompt missing coder output from previous cycle")
	}
	if !strings.Contains(prompt, "missing error check") {
		t.Error("prompt missing reviewer findings")
	}

	// Flag should be cleared after prompt is built.
	if state.Refactored {
		t.Error("Refactored should be false after buildCoderPrompt")
	}
	if state.OriginalDescription != "" {
		t.Errorf("OriginalDescription should be empty, got %q", state.OriginalDescription)
	}
	if state.RefactorDescription != "" {
		t.Errorf("RefactorDescription should be empty, got %q", state.RefactorDescription)
	}
}

func TestBuildCoderPrompt_NotRefactored(t *testing.T) {
	t.Parallel()
	l := &Loop{}
	state := &CycleState{
		TaskBeadID: "bead-456",
		TaskTitle:  "normal task",
		Cycle:      1,
		Refactored: false,
	}

	prompt := l.buildCoderPrompt(state)

	// Normal prompt should NOT contain REFACTOR section.
	if strings.Contains(prompt, "[REFACTOR") {
		t.Error("normal prompt should not contain [REFACTOR] section")
	}
	if !strings.Contains(prompt, "normal task") {
		t.Error("prompt missing task title")
	}
	if !strings.Contains(prompt, "Implement this task") {
		t.Error("prompt missing implementation instruction for cycle 1")
	}
}

func TestBuildCoderPrompt_RefactorClearsAfterOneCycle(t *testing.T) {
	t.Parallel()
	l := &Loop{}
	state := &CycleState{
		TaskBeadID:          "bead-789",
		TaskTitle:           "v2 task",
		Cycle:               3,
		Refactored:          true,
		OriginalDescription: "v1 task",
		RefactorDescription: "v2 task",
		Findings: []ReviewFinding{
			{Severity: "minor", Description: "style issue"},
		},
	}

	// First call uses the refactor prompt.
	first := l.buildCoderPrompt(state)
	if !strings.Contains(first, "[REFACTOR — USER UPDATE]") {
		t.Error("first prompt should contain REFACTOR section")
	}

	// Second call with same state should use normal prompt.
	state.Cycle = 4
	state.Findings = []ReviewFinding{{Severity: "minor", Description: "new issue"}}
	second := l.buildCoderPrompt(state)
	if strings.Contains(second, "[REFACTOR") {
		t.Error("second prompt should not contain REFACTOR section")
	}
	if !strings.Contains(second, "reviewer found issues") {
		t.Error("second prompt should contain normal reviewer-issues text")
	}
}

func TestBuildRefactorPrompt_NoPreviousWork(t *testing.T) {
	t.Parallel()
	l := &Loop{}
	state := &CycleState{
		TaskBeadID:          "bead-abc",
		TaskTitle:           "new task",
		Cycle:               1,
		Refactored:          true,
		OriginalDescription: "old task",
		RefactorDescription: "new task",
		CoderOutput:         "",
		Findings:            nil,
	}

	prompt := l.buildCoderPrompt(state)

	if !strings.Contains(prompt, "[REFACTOR — USER UPDATE]") {
		t.Error("prompt missing REFACTOR section")
	}
	// Should NOT contain PREVIOUS WORK section when there's no prior output.
	if strings.Contains(prompt, "[PREVIOUS WORK]") {
		t.Error("prompt should not contain [PREVIOUS WORK] when no prior output exists")
	}
}

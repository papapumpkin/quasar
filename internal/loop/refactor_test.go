package loop

import "testing"

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

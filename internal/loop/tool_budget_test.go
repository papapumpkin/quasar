package loop

import (
	"strings"
	"testing"
)

func TestBudgetReadsBeforeEdit(t *testing.T) {
	t.Parallel()

	t.Run("advisory fires on the read past the limit", func(t *testing.T) {
		t.Parallel()
		b := DefaultBudget() // MaxReadsBeforeEdit = 8
		// Reads 1..8 must not advise.
		for i := 1; i <= b.MaxReadsBeforeEdit; i++ {
			proceed, advisory := b.OnToolCall(ToolCall{Name: "Read"})
			if !proceed {
				t.Fatalf("read %d should proceed", i)
			}
			if advisory != "" {
				t.Fatalf("read %d should not advise, got %q", i, advisory)
			}
		}
		// The 9th read crosses the limit.
		proceed, advisory := b.OnToolCall(ToolCall{Name: "Read"})
		if !proceed {
			t.Error("soft advisory must still allow the call to proceed")
		}
		if advisory == "" {
			t.Fatal("expected advisory after exceeding MaxReadsBeforeEdit")
		}
		if !strings.Contains(advisory, "<system-reminder>") {
			t.Errorf("advisory should be a system-reminder note, got %q", advisory)
		}
		if !strings.Contains(advisory, "Edit") {
			t.Errorf("advisory should mention committing to an edit, got %q", advisory)
		}
	})

	t.Run("zero reads produces zero advisories", func(t *testing.T) {
		t.Parallel()
		b := DefaultBudget()
		_, advisory := b.OnToolCall(ToolCall{Name: "Edit"})
		if advisory != "" {
			t.Errorf("edit should not advise, got %q", advisory)
		}
	})

	t.Run("edit resets the reads-before-edit counter", func(t *testing.T) {
		t.Parallel()
		b := DefaultBudget()
		for i := 0; i < b.MaxReadsBeforeEdit; i++ {
			b.OnToolCall(ToolCall{Name: "Read"})
		}
		b.OnToolCall(ToolCall{Name: "Edit"})
		// After an edit, a fresh read must not immediately advise.
		_, advisory := b.OnToolCall(ToolCall{Name: "Read"})
		if advisory != "" {
			t.Errorf("read after edit reset should not advise, got %q", advisory)
		}
	})

	t.Run("advisory text is deterministic", func(t *testing.T) {
		t.Parallel()
		mk := func() string {
			b := DefaultBudget()
			var last string
			for i := 0; i <= b.MaxReadsBeforeEdit; i++ {
				_, last = b.OnToolCall(ToolCall{Name: "Read"})
			}
			return last
		}
		if mk() != mk() {
			t.Error("advisory text must be deterministic for golden assertions")
		}
	})
}

func TestBudgetTotalReadsHardCap(t *testing.T) {
	t.Parallel()

	b := DefaultBudget() // MaxTotalReads = 30
	for i := 0; i < b.MaxTotalReads; i++ {
		proceed, _ := b.OnToolCall(ToolCall{Name: "Read"})
		if !proceed {
			t.Fatalf("read %d within cap should proceed", i+1)
		}
		// Keep the soft reads-before-edit counter from interfering by editing
		// periodically; the hard cap counts total reads regardless.
		if i%4 == 3 {
			b.OnToolCall(ToolCall{Name: "Edit"})
		}
	}
	proceed, advisory := b.OnToolCall(ToolCall{Name: "Read"})
	if proceed {
		t.Error("read beyond MaxTotalReads must be rejected (proceed=false)")
	}
	if advisory == "" {
		t.Error("hard-cap rejection should carry an explanatory advisory")
	}
}

func TestBudgetGrepsBeforeEdit(t *testing.T) {
	t.Parallel()

	b := DefaultBudget() // MaxGrepsBeforeEdit = 6
	for i := 1; i <= b.MaxGrepsBeforeEdit; i++ {
		_, advisory := b.OnToolCall(ToolCall{Name: "Grep"})
		if advisory != "" {
			t.Fatalf("grep %d should not advise", i)
		}
	}
	_, advisory := b.OnToolCall(ToolCall{Name: "Grep"})
	if advisory == "" {
		t.Error("expected advisory after exceeding MaxGrepsBeforeEdit")
	}
}

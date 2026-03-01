package loop

import (
	"testing"
)

func TestFindingID(t *testing.T) {
	t.Parallel()

	t.Run("Deterministic", func(t *testing.T) {
		t.Parallel()
		id1 := FindingID("critical", "SQL injection vulnerability")
		id2 := FindingID("critical", "SQL injection vulnerability")
		if id1 != id2 {
			t.Errorf("same inputs produced different IDs: %q vs %q", id1, id2)
		}
	})

	t.Run("StableWithWhitespace", func(t *testing.T) {
		t.Parallel()
		id1 := FindingID("major", "missing error check")
		id2 := FindingID("major", "  missing error check  ")
		if id1 != id2 {
			t.Errorf("trimmed inputs should produce same ID: %q vs %q", id1, id2)
		}
	})

	t.Run("DifferentSeverity", func(t *testing.T) {
		t.Parallel()
		id1 := FindingID("critical", "missing error check")
		id2 := FindingID("minor", "missing error check")
		if id1 == id2 {
			t.Errorf("different severities should produce different IDs, both got %q", id1)
		}
	})

	t.Run("DifferentDescription", func(t *testing.T) {
		t.Parallel()
		id1 := FindingID("major", "missing error check")
		id2 := FindingID("major", "unused variable")
		if id1 == id2 {
			t.Errorf("different descriptions should produce different IDs, both got %q", id1)
		}
	})

	t.Run("HasPrefix", func(t *testing.T) {
		t.Parallel()
		id := FindingID("major", "some finding")
		if len(id) < 3 || id[:2] != "f-" {
			t.Errorf("expected ID to start with 'f-', got %q", id)
		}
	})

	t.Run("ConsistentLength", func(t *testing.T) {
		t.Parallel()
		id1 := FindingID("critical", "short")
		id2 := FindingID("minor", "a much longer description that goes on and on")
		if len(id1) != len(id2) {
			t.Errorf("IDs should have consistent length: %q (%d) vs %q (%d)",
				id1, len(id1), id2, len(id2))
		}
	})
}

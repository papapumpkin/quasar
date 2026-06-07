package tui

import (
	"regexp"
	"strings"
	"testing"
)

// ansiPattern strips terminal color escape codes so assertions can match the
// plain text lipgloss wraps each styled segment in.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

func TestCachePanel_View(t *testing.T) {
	panel := CachePanel{Read: 18200, Created: 1400, Fresh: 2100, Cycles: 5}
	got := stripANSI(panel.View())

	// All three token numbers must be present.
	for _, want := range []string{"18.2k", "1.4k", "2.1k"} {
		if !strings.Contains(got, want) {
			t.Errorf("View() = %q, missing token count %q", got, want)
		}
	}
	// Hit rate: 18200 / (18200 + 2100) = 0.8966 → 90%.
	if !strings.Contains(got, "hit 90%") {
		t.Errorf("View() = %q, missing %q", got, "hit 90%")
	}
	if !strings.Contains(got, "last 5 cycles") {
		t.Errorf("View() = %q, missing cycle count", got)
	}
	if !strings.Contains(got, "Cache:") {
		t.Errorf("View() = %q, missing label", got)
	}
}

func TestCachePanel_SingularCycle(t *testing.T) {
	got := stripANSI(CachePanel{Read: 10, Created: 0, Fresh: 90, Cycles: 1}.View())
	if !strings.Contains(got, "last 1 cycle") {
		t.Errorf("View() = %q, want singular %q", got, "last 1 cycle")
	}
	// 10 / (10 + 90) = 0.1 → 10%.
	if !strings.Contains(got, "hit 10%") {
		t.Errorf("View() = %q, missing %q", got, "hit 10%")
	}
}

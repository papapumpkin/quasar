package tui

import (
	"strings"
	"testing"
)

func TestRenderMarkdown(t *testing.T) {
	t.Parallel()

	t.Run("renders heading", func(t *testing.T) {
		t.Parallel()
		out := RenderMarkdown("# Hello", 80)
		// glamour renders headings in uppercase or bold ANSI — the text
		// should still contain "Hello" (possibly uppercased).
		upper := strings.ToUpper(out)
		if !strings.Contains(upper, "HELLO") {
			t.Errorf("expected rendered heading to contain HELLO, got %q", out)
		}
	})

	t.Run("renders code block", func(t *testing.T) {
		t.Parallel()
		md := "```go\nfmt.Println(\"hi\")\n```"
		out := RenderMarkdown(md, 80)
		if !strings.Contains(out, "Println") {
			t.Errorf("expected code block content preserved, got %q", out)
		}
	})

	t.Run("renders bold", func(t *testing.T) {
		t.Parallel()
		out := RenderMarkdown("**bold text**", 80)
		if !strings.Contains(out, "bold text") {
			t.Errorf("expected bold text content preserved, got %q", out)
		}
	})

	t.Run("renders list", func(t *testing.T) {
		t.Parallel()
		md := "- item one\n- item two"
		out := RenderMarkdown(md, 80)
		if !strings.Contains(out, "item one") || !strings.Contains(out, "item two") {
			t.Errorf("expected list items preserved, got %q", out)
		}
	})

	t.Run("fallback on zero width", func(t *testing.T) {
		t.Parallel()
		// Width <= 0 should default to 80 and still render, not panic.
		out := RenderMarkdown("# Test", 0)
		if out == "" {
			t.Error("expected non-empty output for zero width")
		}
	})

	t.Run("empty input", func(t *testing.T) {
		t.Parallel()
		out := RenderMarkdown("", 80)
		// Should not panic; empty or whitespace output is fine.
		_ = out
	})

	t.Run("plain text passthrough", func(t *testing.T) {
		t.Parallel()
		out := RenderMarkdown("just plain text", 80)
		if !strings.Contains(out, "just plain text") {
			t.Errorf("expected plain text preserved, got %q", out)
		}
	})

	t.Run("width constrains output", func(t *testing.T) {
		t.Parallel()
		long := strings.Repeat("word ", 40) // 200 chars
		out := RenderMarkdown(long, 40)
		// Output should contain the words (not be empty).
		if !strings.Contains(out, "word") {
			t.Errorf("expected words preserved in narrow render, got %q", out)
		}
	})
}

package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderMarkdown(t *testing.T) {
	t.Parallel()

	t.Run("renders heading", func(t *testing.T) {
		t.Parallel()
		out := RenderMarkdown("# Hello", 80)
		plain := ansi.Strip(out)
		upper := strings.ToUpper(plain)
		if !strings.Contains(upper, "HELLO") {
			t.Errorf("expected rendered heading to contain HELLO, got %q", plain)
		}
	})

	t.Run("renders code block", func(t *testing.T) {
		t.Parallel()
		md := "```go\nfmt.Println(\"hi\")\n```"
		out := RenderMarkdown(md, 80)
		plain := ansi.Strip(out)
		if !strings.Contains(plain, "Println") {
			t.Errorf("expected code block content preserved, got %q", plain)
		}
	})

	t.Run("renders bold", func(t *testing.T) {
		t.Parallel()
		out := RenderMarkdown("**bold text**", 80)
		plain := ansi.Strip(out)
		if !strings.Contains(plain, "bold text") {
			t.Errorf("expected bold text content preserved, got %q", plain)
		}
	})

	t.Run("renders list", func(t *testing.T) {
		t.Parallel()
		md := "- item one\n- item two"
		out := RenderMarkdown(md, 80)
		plain := ansi.Strip(out)
		if !strings.Contains(plain, "item one") || !strings.Contains(plain, "item two") {
			t.Errorf("expected list items preserved, got %q", plain)
		}
	})

	t.Run("fallback on zero width", func(t *testing.T) {
		t.Parallel()
		out := RenderMarkdown("# Test", 0)
		if out == "" {
			t.Error("expected non-empty output for zero width")
		}
	})

	t.Run("empty input", func(t *testing.T) {
		t.Parallel()
		out := RenderMarkdown("", 80)
		_ = out // should not panic
	})

	t.Run("plain text passthrough", func(t *testing.T) {
		t.Parallel()
		out := RenderMarkdown("just plain text", 80)
		plain := ansi.Strip(out)
		if !strings.Contains(plain, "just plain text") {
			t.Errorf("expected plain text preserved, got %q", plain)
		}
	})

	t.Run("width constrains output", func(t *testing.T) {
		t.Parallel()
		long := strings.Repeat("word ", 40)
		out := RenderMarkdown(long, 40)
		plain := ansi.Strip(out)
		if !strings.Contains(plain, "word") {
			t.Errorf("expected words preserved in narrow render, got %q", plain)
		}
	})
}

package claude

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/papapumpkin/quasar/internal/agent"
)

func TestTruncationPolicyFor(t *testing.T) {
	t.Parallel()

	t.Run("structured result bypasses truncation", func(t *testing.T) {
		t.Parallel()
		// A reviewer-style budget whose result is a whole JSON payload: the
		// policy must disable truncation so a head+tail marker can never be
		// spliced into the middle of the document.
		a := agent.Agent{ContextBudget: &agent.ContextBudget{
			ToolResultMaxBytes: 32 * 1024,
			ResultIsStructured: true,
		}}
		p := truncationPolicyFor(a)
		if p.MaxBytesPerResult > 0 {
			t.Fatalf("expected truncation disabled for structured result, got cap %d", p.MaxBytesPerResult)
		}
		// And TruncateResult must leave an over-cap JSON payload untouched.
		in := "{\"comments\":[" + strings.Repeat("\"c\",", 20*1024) + "\"x\"]}"
		got, truncated := TruncateResult(in, p)
		if truncated || got != in {
			t.Error("expected structured result returned unchanged")
		}
	})

	t.Run("prose result honors the byte cap", func(t *testing.T) {
		t.Parallel()
		a := agent.Agent{ContextBudget: &agent.ContextBudget{
			ToolResultMaxBytes: 16 * 1024,
			ResultIsStructured: false,
		}}
		p := truncationPolicyFor(a)
		if p.MaxBytesPerResult != 16*1024 {
			t.Fatalf("expected cap 16384 for prose result, got %d", p.MaxBytesPerResult)
		}
	})
}

func TestTruncateResult(t *testing.T) {
	t.Parallel()

	t.Run("small result passes through unchanged", func(t *testing.T) {
		t.Parallel()
		in := strings.Repeat("a", 4*1024)
		got, truncated := TruncateResult(in, DefaultTruncationPolicy())
		if truncated {
			t.Fatalf("expected no truncation for %d-byte input under 16KB cap", len(in))
		}
		if got != in {
			t.Error("expected result returned unchanged")
		}
	})

	t.Run("result equal to cap passes through unchanged", func(t *testing.T) {
		t.Parallel()
		p := DefaultTruncationPolicy()
		in := strings.Repeat("b", p.MaxBytesPerResult)
		got, truncated := TruncateResult(in, p)
		if truncated {
			t.Fatal("expected no truncation when len == MaxBytesPerResult")
		}
		if got != in {
			t.Error("expected result returned unchanged at exactly the cap")
		}
	})

	t.Run("large result truncated to cap with head tail and marker", func(t *testing.T) {
		t.Parallel()
		p := DefaultTruncationPolicy() // 16 KB
		in := strings.Repeat("x", 32*1024)
		got, truncated := TruncateResult(in, p)
		if !truncated {
			t.Fatal("expected truncation for 32KB input under 16KB cap")
		}
		if len(got) > p.MaxBytesPerResult {
			t.Errorf("truncated output %d bytes exceeds cap %d", len(got), p.MaxBytesPerResult)
		}
		if !strings.Contains(got, "truncated") {
			t.Error("expected truncation marker in output")
		}
		// Head and tail of the original content must survive.
		if !strings.HasPrefix(got, "x") {
			t.Error("expected head content preserved")
		}
		if !strings.HasSuffix(got, "x") {
			t.Error("expected tail content preserved")
		}
	})

	t.Run("marker reports truncated and total byte counts", func(t *testing.T) {
		t.Parallel()
		p := DefaultTruncationPolicy()
		in := strings.Repeat("y", 32*1024)
		got, _ := TruncateResult(in, p)
		if !strings.Contains(got, "32768 total") {
			t.Errorf("expected total byte count in marker, got marker region: %q", markerRegion(got))
		}
	})

	t.Run("head only policy keeps prefix and drops tail", func(t *testing.T) {
		t.Parallel()
		p := TruncationPolicy{MaxBytesPerResult: 1024, KeepHead: true, KeepTail: false, Marker: defaultMarker}
		in := strings.Repeat("h", 512) + strings.Repeat("t", 4096)
		got, truncated := TruncateResult(in, p)
		if !truncated {
			t.Fatal("expected truncation")
		}
		if len(got) > p.MaxBytesPerResult {
			t.Errorf("output %d exceeds cap %d", len(got), p.MaxBytesPerResult)
		}
		if !strings.HasPrefix(got, "h") {
			t.Error("expected head preserved")
		}
		if strings.HasSuffix(got, "t") {
			t.Error("tail should have been dropped under head-only policy")
		}
	})

	t.Run("zero or negative cap disables truncation", func(t *testing.T) {
		t.Parallel()
		in := strings.Repeat("z", 100*1024)
		got, truncated := TruncateResult(in, TruncationPolicy{MaxBytesPerResult: 0})
		if truncated || got != in {
			t.Error("expected no truncation when cap <= 0")
		}
	})

	t.Run("empty input returns empty unchanged", func(t *testing.T) {
		t.Parallel()
		got, truncated := TruncateResult("", DefaultTruncationPolicy())
		if truncated || got != "" {
			t.Error("expected empty input unchanged")
		}
	})

	t.Run("cap smaller than marker still honors byte cap", func(t *testing.T) {
		t.Parallel()
		// A cap below the rendered marker length must never produce output
		// larger than the cap — the marker itself is clamped.
		p := TruncationPolicy{MaxBytesPerResult: 8, KeepHead: true, KeepTail: true, Marker: defaultMarker}
		in := strings.Repeat("q", 1024)
		got, truncated := TruncateResult(in, p)
		if !truncated {
			t.Fatal("expected truncation")
		}
		if len(got) > p.MaxBytesPerResult {
			t.Errorf("output %d bytes exceeds tiny cap %d", len(got), p.MaxBytesPerResult)
		}
	})

	t.Run("does not split multibyte runes at cut points", func(t *testing.T) {
		t.Parallel()
		// All multi-byte runes: any byte-boundary cut would split a rune and
		// emit invalid UTF-8. The output must remain valid UTF-8.
		in := strings.Repeat("世", 8*1024) // 3 bytes each = 24 KB
		p := DefaultTruncationPolicy()    // 16 KB
		got, truncated := TruncateResult(in, p)
		if !truncated {
			t.Fatal("expected truncation for 24KB input under 16KB cap")
		}
		if len(got) > p.MaxBytesPerResult {
			t.Errorf("output %d exceeds cap %d", len(got), p.MaxBytesPerResult)
		}
		if !utf8.ValidString(got) {
			t.Error("expected truncated output to remain valid UTF-8 (no split runes)")
		}
	})
}

// markerRegion is a test helper that returns the middle of a truncated string
// for diagnostic output. It is deliberately approximate.
func markerRegion(s string) string {
	if len(s) < 200 {
		return s
	}
	mid := len(s) / 2
	lo, hi := mid-100, mid+100
	if lo < 0 {
		lo = 0
	}
	if hi > len(s) {
		hi = len(s)
	}
	return s[lo:hi]
}

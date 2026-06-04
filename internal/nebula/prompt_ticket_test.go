package nebula

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/integrations"
)

// indexOrFail returns the index of sub within s, failing the test if absent.
func indexOrFail(t *testing.T, s, sub string) int {
	t.Helper()
	idx := strings.Index(s, sub)
	if idx < 0 {
		t.Fatalf("expected prompt to contain %q", sub)
	}
	return idx
}

func TestRenderTicketPrompt(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	later := time.Date(2026, 6, 2, 9, 30, 0, 0, time.UTC)

	tests := []struct {
		name        string
		ticket      *integrations.Ticket
		wantContain []string // substrings that must appear
		wantOrder   []string // substrings that must appear in this relative order
		wantAbsent  []string // substrings that must NOT appear
	}{
		{
			name: "minimal fields",
			ticket: &integrations.Ticket{
				SourceName: "github",
				SourceID:   "papapumpkin/quasar#42",
				Number:     42,
				Title:      "Fix truncate off-by-one",
				Body:       "The truncate helper drops one byte too many.",
				State:      "open",
				URL:        "https://github.com/papapumpkin/quasar/issues/42",
			},
			wantContain: []string{
				"Source: github",
				"Reference: papapumpkin/quasar#42",
				"URL: https://github.com/papapumpkin/quasar/issues/42",
				"Title: Fix truncate off-by-one",
				"State: open",
				"The truncate helper drops one byte too many.",
			},
			wantOrder: []string{
				"Reference: papapumpkin/quasar#42",
				"URL: https://github.com/papapumpkin/quasar/issues/42",
				"Title: Fix truncate off-by-one",
				"--- Ticket Body ---",
				"The truncate helper drops one byte too many.",
			},
			wantAbsent: []string{"Labels:", "Assigned to:", "--- Comments", "--- Linked Work"},
		},
		{
			name: "all fields with comments and linked work",
			ticket: &integrations.Ticket{
				SourceName: "github",
				SourceID:   "papapumpkin/quasar#7",
				Number:     7,
				Title:      "Add config validation",
				Body:       "We need stricter config validation.",
				State:      "open",
				Labels:     []string{"enhancement", "config"},
				Assignee:   "octocat",
				URL:        "https://example.com/7",
				Comments: []integrations.Comment{
					{Author: "alice", Body: "First comment.", CreatedAt: created},
					{Author: "bob", Body: "Second comment.", CreatedAt: later},
				},
				LinkedWork: []string{"https://example.com/pr/9", "papapumpkin/quasar#3"},
			},
			wantContain: []string{
				"Labels: enhancement, config",
				"Assigned to: octocat",
				"--- Comments (2) ---",
				"[alice @ 2026-06-01]",
				"First comment.",
				"[bob @ 2026-06-02]",
				"Second comment.",
				"--- Linked Work (for context, do not reopen) ---",
				"https://example.com/pr/9",
				"papapumpkin/quasar#3",
			},
			wantOrder: []string{
				"Title: Add config validation",
				"--- Ticket Body ---",
				"We need stricter config validation.",
				"--- Comments (2) ---",
				"First comment.",
				"Second comment.",
				"--- Linked Work (for context, do not reopen) ---",
				"https://example.com/pr/9",
			},
		},
		{
			name: "empty body still renders coherently",
			ticket: &integrations.Ticket{
				SourceName: "jira",
				SourceID:   "PROJ-100",
				Number:     0,
				Title:      "Investigate flaky test",
				Body:       "",
				State:      "open",
			},
			wantContain: []string{
				"Title: Investigate flaky test",
				"--- Ticket Body ---",
				"Now produce the nebula manifest",
			},
			wantAbsent: []string{"--- Comments", "--- Linked Work"},
		},
		{
			name: "multi-paragraph body with backticks",
			ticket: &integrations.Ticket{
				SourceName: "github",
				SourceID:   "papapumpkin/quasar#88",
				Number:     88,
				Title:      "Refactor `slugify`",
				Body:       "First paragraph mentions `slugifyID`.\n\nSecond paragraph has a block:\n\n```go\nfunc slugify(s string) string\n```\n\nThird paragraph.",
				State:      "open",
			},
			wantContain: []string{
				"Title: Refactor `slugify`",
				"First paragraph mentions `slugifyID`.",
				"```go",
				"func slugify(s string) string",
				"Third paragraph.",
			},
			wantOrder: []string{
				"First paragraph mentions `slugifyID`.",
				"Second paragraph has a block:",
				"Third paragraph.",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := RenderTicketPrompt(tt.ticket)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for _, sub := range tt.wantContain {
				if !strings.Contains(got, sub) {
					t.Errorf("prompt missing %q\n--- prompt ---\n%s", sub, got)
				}
			}
			for _, sub := range tt.wantAbsent {
				if strings.Contains(got, sub) {
					t.Errorf("prompt unexpectedly contains %q", sub)
				}
			}
			prev := -1
			for _, sub := range tt.wantOrder {
				idx := indexOrFail(t, got, sub)
				if idx < prev {
					t.Errorf("expected %q after the previous ordered element\n--- prompt ---\n%s", sub, got)
				}
				prev = idx
			}
		})
	}
}

func TestRenderTicketPrompt_NilTicket(t *testing.T) {
	t.Parallel()
	if _, err := RenderTicketPrompt(nil); err == nil {
		t.Fatal("expected error for nil ticket")
	}
}

func TestSlugifyTicket(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		ticket *integrations.Ticket
		want   string
	}{
		{
			name:   "numeric ticket",
			ticket: &integrations.Ticket{Number: 42, Title: "Fix truncate off-by-one"},
			want:   "nebula-42-fix-truncate-off-by-one",
		},
		{
			name:   "no number uses slugified source id",
			ticket: &integrations.Ticket{Number: 0, SourceID: "PROJ-100", Title: "Flaky test"},
			want:   "nebula-proj-100-flaky-test",
		},
		{
			name:   "source id with colon and slash collapses to hyphens",
			ticket: &integrations.Ticket{Number: 0, SourceID: "team:abc/xyz", Title: "Do thing"},
			want:   "nebula-team-abc-xyz-do-thing",
		},
		{
			name:   "empty ref falls back to ticket",
			ticket: &integrations.Ticket{Number: 0, SourceID: "", Title: "Title only"},
			want:   "nebula-ticket-title-only",
		},
		{
			name:   "title only no number no source",
			ticket: &integrations.Ticket{Number: 0, Title: ""},
			want:   "nebula-ticket",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := slugifyTicket(tt.ticket)
			if got != tt.want {
				t.Errorf("slugifyTicket = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSlugifyTicket_TruncatesAndStaysASCII(t *testing.T) {
	t.Parallel()

	ticket := &integrations.Ticket{
		Number: 1234,
		Title:  "An extremely long ticket title that should be truncated well past the limit",
	}
	got := slugifyTicket(ticket)

	if len(got) > maxTicketNebulaName {
		t.Errorf("name %q has length %d, want <= %d", got, len(got), maxTicketNebulaName)
	}
	if strings.HasSuffix(got, "-") {
		t.Errorf("name %q must not end with a hyphen", got)
	}
	for _, r := range got {
		isLowerAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if !isLowerAlnum && r != '-' {
			t.Errorf("name %q contains non-ASCII-slug rune %q", got, r)
		}
	}
}

// ticketCannedOutput is a deterministic two-phase architect response used to
// exercise FromTicket without a real LLM.
const ticketCannedOutput = `PHASE_FILE: 01-fix.md
+++
id = "fix-truncate"
title = "Fix Truncate"
scope = ["internal/ansi/**"]
+++

## Problem

Truncate drops a byte.

## Solution

Off-by-one fix.

## Acceptance Criteria

- [ ] Fixed
END_PHASE_FILE

PHASE_FILE: 02-tests.md
+++
id = "add-tests"
title = "Add Tests"
depends_on = ["fix-truncate"]
scope = ["internal/ansi/**"]
+++

## Problem

No coverage.

## Solution

Add tests.

## Acceptance Criteria

- [ ] Covered
END_PHASE_FILE
`

func TestFromTicket(t *testing.T) {
	t.Parallel()

	mock := &mockInvoker{
		result: agent.InvocationResult{ResultText: ticketCannedOutput, CostUSD: 0.02},
	}

	ticket := &integrations.Ticket{
		SourceName: "github",
		SourceID:   "papapumpkin/quasar#42",
		Number:     42,
		Title:      "Fix truncate off-by-one",
		Body:       "The truncate helper drops one byte too many.",
		State:      "open",
		URL:        "https://github.com/papapumpkin/quasar/issues/42",
	}

	outDir := t.TempDir()
	info, err := FromTicket(context.Background(), mock, ticket, outDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Directory name follows the nebula-<number>-<slug> convention.
	if info.Name != "nebula-42-fix-truncate-off-by-one" {
		t.Errorf("name = %q, want %q", info.Name, "nebula-42-fix-truncate-off-by-one")
	}
	if want := filepath.Join(outDir, info.Name); info.Path != want {
		t.Errorf("path = %q, want %q", info.Path, want)
	}

	// The architect received the rendered ticket prompt, not a raw string.
	if !strings.Contains(mock.lastPrompt, "Title: Fix truncate off-by-one") {
		t.Errorf("architect prompt missing ticket title; got:\n%s", mock.lastPrompt)
	}
	if !strings.Contains(mock.lastPrompt, "Reference: papapumpkin/quasar#42") {
		t.Errorf("architect prompt missing ticket reference; got:\n%s", mock.lastPrompt)
	}

	// Provenance is recorded on the in-memory nebula.
	if info.Result.Nebula.SourceName != "github" || info.Result.Nebula.SourceID != "papapumpkin/quasar#42" {
		t.Errorf("nebula provenance = %q/%q, want github/papapumpkin/quasar#42",
			info.Result.Nebula.SourceName, info.Result.Nebula.SourceID)
	}

	// The written nebula.toml records source attribution.
	manifestBytes, err := os.ReadFile(filepath.Join(info.Path, "nebula.toml"))
	if err != nil {
		t.Fatalf("reading written nebula.toml: %v", err)
	}
	manifest := string(manifestBytes)
	for _, want := range []string{
		`source_name = 'github'`,
		`source_id = 'papapumpkin/quasar#42'`,
	} {
		if !strings.Contains(manifest, want) {
			t.Errorf("nebula.toml missing %q\n--- manifest ---\n%s", want, manifest)
		}
	}

	// Two phase files plus the manifest were written.
	if len(info.Result.Phases) != 2 {
		t.Errorf("expected 2 phases, got %d", len(info.Result.Phases))
	}
}

func TestFromTicket_NilTicket(t *testing.T) {
	t.Parallel()
	mock := &mockInvoker{}
	if _, err := FromTicket(context.Background(), mock, nil, t.TempDir()); err == nil {
		t.Fatal("expected error for nil ticket")
	}
}

func TestFromTicket_EmptyOutDir(t *testing.T) {
	t.Parallel()
	mock := &mockInvoker{}
	ticket := &integrations.Ticket{Number: 1, Title: "x"}
	if _, err := FromTicket(context.Background(), mock, ticket, ""); err == nil {
		t.Fatal("expected error for empty output directory")
	}
}

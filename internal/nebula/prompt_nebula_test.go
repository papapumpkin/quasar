package nebula

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
)

// fakeSeedReader is a SeedNebulaReader stub: it returns the configured seed (or
// error) regardless of the requested id, recording the id it was asked for.
type fakeSeedReader struct {
	seed   *SeedNebula
	err    error
	lastID string
}

func (f *fakeSeedReader) ReadSeedNebula(_ context.Context, nebulaID string) (*SeedNebula, error) {
	f.lastID = nebulaID
	return f.seed, f.err
}

// sampleSeed returns a fully-populated seed nebula for renderer assertions.
func sampleSeed() *SeedNebula {
	return &SeedNebula{
		Name:        "Fix truncate off-by-one",
		Description: "The truncate helper drops one byte too many.",
		SourceName:  "github",
		SourceID:    "papapumpkin/quasar#42",
		SourceURL:   "https://github.com/papapumpkin/quasar/issues/42",
		Goals:       []string{"Correct the boundary math", "Preserve UTF-8 runes"},
		Constraints: []string{"No new dependencies", "Keep the public signature"},
		Labels:      []string{"bug", "good-first-issue"},
		Assignee:    "octocat",
	}
}

func TestRenderNebulaPrompt(t *testing.T) {
	t.Parallel()

	t.Run("flattens all seed fields", func(t *testing.T) {
		t.Parallel()
		out, err := RenderNebulaPrompt(sampleSeed())
		if err != nil {
			t.Fatalf("RenderNebulaPrompt: %v", err)
		}
		// Every column the architect needs must appear in the rendered prompt.
		wants := []string{
			"Source: github",
			"Reference: papapumpkin/quasar#42",
			"URL: https://github.com/papapumpkin/quasar/issues/42",
			"Title: Fix truncate off-by-one",
			"Labels: bug, good-first-issue",
			"Assigned to: octocat",
			"The truncate helper drops one byte too many.",
			"Correct the boundary math",
			"Preserve UTF-8 runes",
			"No new dependencies",
			"Keep the public signature",
		}
		for _, w := range wants {
			if !strings.Contains(out, w) {
				t.Errorf("rendered prompt missing %q\n---\n%s", w, out)
			}
		}
	})

	t.Run("omits optional sections when empty", func(t *testing.T) {
		t.Parallel()
		seed := &SeedNebula{
			Name:       "Bare seed",
			SourceName: "github",
			SourceID:   "papapumpkin/quasar#7",
		}
		out, err := RenderNebulaPrompt(seed)
		if err != nil {
			t.Fatalf("RenderNebulaPrompt: %v", err)
		}
		if strings.Contains(out, "Labels:") {
			t.Errorf("expected no Labels line for an unlabeled seed:\n%s", out)
		}
		if strings.Contains(out, "Assigned to:") {
			t.Errorf("expected no assignee line for an unassigned seed:\n%s", out)
		}
	})

	t.Run("nil seed errors", func(t *testing.T) {
		t.Parallel()
		if _, err := RenderNebulaPrompt(nil); err == nil {
			t.Error("expected an error for a nil seed")
		}
	})
}

func TestFromNebula(t *testing.T) {
	t.Parallel()

	// validMultiPhaseOutput is architect output runGenerate can parse into one
	// valid phase, letting FromNebula reach the WriteNebula step.
	const validMultiPhaseOutput = `PHASE_FILE: 01-fix-truncate.md
+++
id = "fix-truncate"
title = "Fix the truncate boundary"
+++

## Problem

Off-by-one in truncate.

## Acceptance Criteria

- [ ] Boundary is correct
END_PHASE_FILE`

	t.Run("happy path writes nebula and preserves provenance", func(t *testing.T) {
		t.Parallel()
		reader := &fakeSeedReader{seed: sampleSeed()}
		invoker := &mockInvoker{result: agent.InvocationResult{ResultText: validMultiPhaseOutput}}
		outDir := t.TempDir()

		gen, err := FromNebula(context.Background(), invoker, reader, "seed-123", outDir)
		if err != nil {
			t.Fatalf("FromNebula: %v", err)
		}
		if reader.lastID != "seed-123" {
			t.Errorf("reader queried id %q, want %q", reader.lastID, "seed-123")
		}
		if !strings.HasPrefix(gen.Name, "nebula-") {
			t.Errorf("generated name %q should start with nebula-", gen.Name)
		}
		if gen.Path != filepath.Join(outDir, gen.Name) {
			t.Errorf("generated path %q, want %q", gen.Path, filepath.Join(outDir, gen.Name))
		}
		if _, statErr := os.Stat(filepath.Join(gen.Path, "nebula.toml")); statErr != nil {
			t.Errorf("expected nebula.toml on disk: %v", statErr)
		}
		// Source provenance from the seed must flow into the written manifest.
		if gen.Result == nil || gen.Result.Nebula == nil {
			t.Fatal("expected a non-nil generated nebula in the result")
		}
		if gen.Result.Nebula.SourceName != "github" || gen.Result.Nebula.SourceID != "papapumpkin/quasar#42" {
			t.Errorf("provenance not preserved: got name=%q id=%q",
				gen.Result.Nebula.SourceName, gen.Result.Nebula.SourceID)
		}
		// The architect prompt the invoker saw must carry the seed context.
		if !strings.Contains(invoker.lastPrompt, "papapumpkin/quasar#42") {
			t.Errorf("architect prompt missing seed source id:\n%s", invoker.lastPrompt)
		}
	})

	t.Run("validates required arguments", func(t *testing.T) {
		t.Parallel()
		reader := &fakeSeedReader{seed: sampleSeed()}
		invoker := &mockInvoker{}
		cases := []struct {
			name     string
			reader   SeedNebulaReader
			nebulaID string
			outDir   string
		}{
			{"nil reader", nil, "id", "out"},
			{"empty id", reader, "", "out"},
			{"empty outDir", reader, "id", ""},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := FromNebula(context.Background(), invoker, tc.reader, tc.nebulaID, tc.outDir); err == nil {
					t.Errorf("expected an error for %s", tc.name)
				}
			})
		}
	})

	t.Run("reader error propagates", func(t *testing.T) {
		t.Parallel()
		reader := &fakeSeedReader{err: context.DeadlineExceeded}
		invoker := &mockInvoker{}
		if _, err := FromNebula(context.Background(), invoker, reader, "id", t.TempDir()); err == nil {
			t.Error("expected the reader error to propagate")
		}
	})

	t.Run("missing seed errors", func(t *testing.T) {
		t.Parallel()
		reader := &fakeSeedReader{seed: nil} // not found, no error
		invoker := &mockInvoker{}
		if _, err := FromNebula(context.Background(), invoker, reader, "id", t.TempDir()); err == nil {
			t.Error("expected an error when the seed is not found")
		}
	})
}

package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/config"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/integrations"
	"github.com/papapumpkin/quasar/internal/nebula"
)

// fakeTicketSource is a stub TicketSource that returns a canned ticket or error.
type fakeTicketSource struct {
	ticket *integrations.Ticket
	err    error
	gotID  string
}

func (f *fakeTicketSource) Name() string { return "fake" }

func (f *fakeTicketSource) Fetch(_ context.Context, sourceID string) (*integrations.Ticket, error) {
	f.gotID = sourceID
	return f.ticket, f.err
}

// fakeArchitect records the target directory it was asked to write into and
// returns a Generated whose name mirrors that directory's basename.
type fakeArchitect struct {
	gotTarget string
	err       error
}

func (f *fakeArchitect) FromTicket(_ context.Context, _ *integrations.Ticket, targetDir string) (*nebula.Generated, error) {
	f.gotTarget = targetDir
	if f.err != nil {
		return nil, f.err
	}
	return &nebula.Generated{Name: filepath.Base(targetDir), Path: targetDir}, nil
}

// fakeRecorder captures inserted rows.
type fakeRecorder struct {
	rows []fabric.NebulaRecord
	err  error
}

func (f *fakeRecorder) InsertNebula(_ context.Context, rec fabric.NebulaRecord) error {
	if f.err != nil {
		return f.err
	}
	f.rows = append(f.rows, rec)
	return nil
}

// githubTicket returns a representative ticket as the github adapter would.
func githubTicket() *integrations.Ticket {
	return &integrations.Ticket{
		SourceName: "github",
		SourceID:   "papapumpkin/quasar#42",
		Number:     42,
		Title:      "Add OAuth",
	}
}

// newDeps assembles nebulaNewDeps with the given source and a github section
// configured, plus an output sink.
func newDeps(src integrations.TicketSource, arch ticketArchitect, rec nebulaRecorder, dirExists func(string) bool, out *strings.Builder) nebulaNewDeps {
	if dirExists == nil {
		dirExists = func(string) bool { return false }
	}
	return nebulaNewDeps{
		cfg: config.Config{
			IntegrationSections: map[string]map[string]any{"github": {}},
		},
		buildSource: func(string, map[string]any) (integrations.TicketSource, error) {
			return src, nil
		},
		architect: arch,
		recorder:  rec,
		dirExists: dirExists,
		out:       out,
	}
}

func TestRunNebulaNewWith_HappyPath(t *testing.T) {
	t.Parallel()

	src := &fakeTicketSource{ticket: githubTicket()}
	arch := &fakeArchitect{}
	rec := &fakeRecorder{}
	var out strings.Builder

	err := runNebulaNewWith(context.Background(), "github:42", "", ".nebulas", newDeps(src, arch, rec, nil, &out))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if src.gotID != "42" {
		t.Errorf("source got id %q, want %q", src.gotID, "42")
	}
	wantTarget := filepath.Join(".nebulas", "nebula-42-add-oauth")
	if arch.gotTarget != wantTarget {
		t.Errorf("architect target = %q, want %q", arch.gotTarget, wantTarget)
	}
	if len(rec.rows) != 1 {
		t.Fatalf("recorded %d rows, want 1", len(rec.rows))
	}
	row := rec.rows[0]
	if row.SourceName != "github" || row.SourceID != "papapumpkin/quasar#42" {
		t.Errorf("row source = %q/%q, want github/papapumpkin/quasar#42", row.SourceName, row.SourceID)
	}
	if row.SourceType != "ticket" || row.Status != "draft" {
		t.Errorf("row type/status = %q/%q, want ticket/draft", row.SourceType, row.Status)
	}
	if row.ID != "nebula-42-add-oauth" || row.Path != wantTarget {
		t.Errorf("row id/path = %q/%q, want nebula-42-add-oauth/%s", row.ID, row.Path, wantTarget)
	}
	if !strings.Contains(out.String(), "created draft nebula at "+wantTarget) {
		t.Errorf("summary = %q, missing created-draft line", out.String())
	}
	if !strings.Contains(out.String(), "source: github, ref: papapumpkin/quasar#42") {
		t.Errorf("summary = %q, missing source/ref", out.String())
	}
}

func TestRunNebulaNewWith_NameOverride(t *testing.T) {
	t.Parallel()

	src := &fakeTicketSource{ticket: githubTicket()}
	arch := &fakeArchitect{}
	var out strings.Builder

	err := runNebulaNewWith(context.Background(), "github:42", "my-custom-name", ".nebulas", newDeps(src, arch, &fakeRecorder{}, nil, &out))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(".nebulas", "my-custom-name")
	if arch.gotTarget != want {
		t.Errorf("architect target = %q, want %q", arch.gotTarget, want)
	}
}

func TestRunNebulaNewWith_BadFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  string
	}{
		{"no colon", "github"},
		{"empty source", ":42"},
		{"empty id", "github:"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			arch := &fakeArchitect{}
			var out strings.Builder
			err := runNebulaNewWith(context.Background(), tc.ref, "", ".nebulas",
				newDeps(&fakeTicketSource{ticket: githubTicket()}, arch, &fakeRecorder{}, nil, &out))
			if err == nil {
				t.Fatalf("expected error for ref %q", tc.ref)
			}
			if !strings.Contains(err.Error(), "invalid ticket reference") {
				t.Errorf("error = %q, want it to mention invalid ticket reference", err.Error())
			}
			if exitCodeOf(err) != 1 {
				t.Errorf("exit code = %d, want 1", exitCodeOf(err))
			}
			if arch.gotTarget != "" {
				t.Error("architect should not be called on bad format")
			}
		})
	}
}

func TestRunNebulaNewWith_UnknownSource(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	deps := newDeps(&fakeTicketSource{ticket: githubTicket()}, &fakeArchitect{}, &fakeRecorder{}, nil, &out)

	err := runNebulaNewWith(context.Background(), "linear:42", "", ".nebulas", deps)
	if err == nil {
		t.Fatal("expected error for unconfigured source")
	}
	if !strings.Contains(err.Error(), "[integrations.linear]") {
		t.Errorf("error = %q, want it to mention the missing config block", err.Error())
	}
	if exitCodeOf(err) != 1 {
		t.Errorf("exit code = %d, want 1", exitCodeOf(err))
	}
}

func TestRunNebulaNewWith_TicketNotFound(t *testing.T) {
	t.Parallel()

	src := &fakeTicketSource{err: fmt.Errorf("github: issue x#999 not found: %w", integrations.ErrTicketNotFound)}
	var out strings.Builder

	err := runNebulaNewWith(context.Background(), "github:999", "", ".nebulas",
		newDeps(src, &fakeArchitect{}, &fakeRecorder{}, nil, &out))
	if err == nil {
		t.Fatal("expected error for missing ticket")
	}
	if exitCodeOf(err) != 2 {
		t.Errorf("exit code = %d, want 2", exitCodeOf(err))
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want a not-found message", err.Error())
	}
}

func TestRunNebulaNewWith_Collision(t *testing.T) {
	t.Parallel()

	base := filepath.Join(".nebulas", "nebula-42-add-oauth")
	// The base directory already exists; the -2 suffix is free.
	exists := func(p string) bool { return p == base }

	src := &fakeTicketSource{ticket: githubTicket()}
	arch := &fakeArchitect{}
	var out strings.Builder

	err := runNebulaNewWith(context.Background(), "github:42", "", ".nebulas",
		newDeps(src, arch, &fakeRecorder{}, exists, &out))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(".nebulas", "nebula-42-add-oauth-2")
	if arch.gotTarget != want {
		t.Errorf("architect target = %q, want %q", arch.gotTarget, want)
	}
}

func TestRunNebulaNewWith_RecorderErrorPropagates(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	rec := &fakeRecorder{err: errors.New("db down")}
	err := runNebulaNewWith(context.Background(), "github:42", "", ".nebulas",
		newDeps(&fakeTicketSource{ticket: githubTicket()}, &fakeArchitect{}, rec, nil, &out))
	if err == nil || !strings.Contains(err.Error(), "record draft nebula") {
		t.Fatalf("error = %v, want it to wrap the recorder failure", err)
	}
}

func TestSplitTicketRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ref        string
		wantSource string
		wantID     string
		wantErr    bool
	}{
		{"github:42", "github", "42", false},
		{"github:owner/repo#42", "github", "owner/repo#42", false},
		{"github", "", "", true},
		{":42", "", "", true},
		{"github:", "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.ref, func(t *testing.T) {
			t.Parallel()
			source, id, err := splitTicketRef(tc.ref)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if err == nil && (source != tc.wantSource || id != tc.wantID) {
				t.Errorf("got %q/%q, want %q/%q", source, id, tc.wantSource, tc.wantID)
			}
		})
	}
}

func TestAddNebulaNewFlags(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "test"}
	addNebulaNewFlags(cmd)

	for _, flag := range []string{"name", "dir"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("flag %q not registered", flag)
		}
	}
	if dv := cmd.Flags().Lookup("dir").DefValue; dv != defaultNebulaDir {
		t.Errorf("dir default = %q, want %q", dv, defaultNebulaDir)
	}
}

func TestNebulaNewRegistered(t *testing.T) {
	t.Parallel()

	found := false
	for _, sub := range nebulaCmd.Commands() {
		if sub.Name() == "new" {
			found = true
			break
		}
	}
	if !found {
		t.Error("new subcommand not registered under nebulaCmd")
	}
}

// exitCodeOf returns the exit code an error requests via *exitCodeError, or 1
// for any other error.
func exitCodeOf(err error) int {
	var ec *exitCodeError
	if errors.As(err, &ec) {
		return ec.code
	}
	return 1
}

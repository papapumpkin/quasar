package beads

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBuildQuickCreateArgs_Base(t *testing.T) {
	t.Parallel()
	args := buildQuickCreateArgs("fix bug", CreateOpts{})

	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d: %v", len(args), args)
	}
	if args[0] != "q" {
		t.Errorf("args[0] = %q, want %q", args[0], "q")
	}
	if args[1] != "fix bug" {
		t.Errorf("args[1] = %q, want %q", args[1], "fix bug")
	}
}

func TestBuildQuickCreateArgs_AllOpts(t *testing.T) {
	t.Parallel()
	args := buildQuickCreateArgs("fix bug", CreateOpts{
		Type:     "bug",
		Labels:   []string{"urgent", "backend"},
		Priority: "1",
	})

	want := []string{"q", "fix bug", "-t", "bug", "-l", "urgent", "-l", "backend", "-p", "1"}
	if len(args) != len(want) {
		t.Fatalf("expected %d args, got %d: %v", len(want), len(args), args)
	}
	for i, w := range want {
		if args[i] != w {
			t.Errorf("args[%d] = %q, want %q", i, args[i], w)
		}
	}
}

func TestBuildQuickCreateArgs_OptionalFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		opts     CreateOpts
		wantFlag string
		present  bool
	}{
		{
			name:     "type present",
			opts:     CreateOpts{Type: "task"},
			wantFlag: "-t",
			present:  true,
		},
		{
			name:     "type absent",
			opts:     CreateOpts{},
			wantFlag: "-t",
			present:  false,
		},
		{
			name:     "priority present",
			opts:     CreateOpts{Priority: "2"},
			wantFlag: "-p",
			present:  true,
		},
		{
			name:     "priority absent",
			opts:     CreateOpts{},
			wantFlag: "-p",
			present:  false,
		},
		{
			name:     "labels present",
			opts:     CreateOpts{Labels: []string{"frontend"}},
			wantFlag: "-l",
			present:  true,
		},
		{
			name:     "labels absent",
			opts:     CreateOpts{},
			wantFlag: "-l",
			present:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			args := buildQuickCreateArgs("title", tt.opts)
			found := false
			for _, arg := range args {
				if arg == tt.wantFlag {
					found = true
					break
				}
			}
			if found != tt.present {
				t.Errorf("flag %q: found=%v, want present=%v (args: %v)", tt.wantFlag, found, tt.present, args)
			}
		})
	}
}

func TestBuildCreateArgs_Base(t *testing.T) {
	t.Parallel()
	args := buildCreateArgs("new feature", CreateOpts{})

	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d: %v", len(args), args)
	}
	if args[0] != "create" {
		t.Errorf("args[0] = %q, want %q", args[0], "create")
	}
	if args[1] != "new feature" {
		t.Errorf("args[1] = %q, want %q", args[1], "new feature")
	}
	if args[2] != "--silent" {
		t.Errorf("args[2] = %q, want %q", args[2], "--silent")
	}
}

func TestBuildCreateArgs_AllOpts(t *testing.T) {
	t.Parallel()
	args := buildCreateArgs("new feature", CreateOpts{
		Description: "a long description",
		Type:        "feature",
		Labels:      []string{"frontend", "v2"},
		Parent:      "beads-001",
		Assignee:    "alice",
		Priority:    "0",
	})

	want := []string{
		"create", "new feature", "--silent",
		"-d", "a long description",
		"-t", "feature",
		"-l", "frontend", "-l", "v2",
		"--parent", "beads-001",
		"-a", "alice",
		"-p", "0",
	}
	if len(args) != len(want) {
		t.Fatalf("expected %d args, got %d: %v", len(want), len(args), args)
	}
	for i, w := range want {
		if args[i] != w {
			t.Errorf("args[%d] = %q, want %q", i, args[i], w)
		}
	}
}

func TestBuildCreateArgs_OptionalFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		opts     CreateOpts
		wantFlag string
		present  bool
	}{
		{
			name:     "description present",
			opts:     CreateOpts{Description: "desc"},
			wantFlag: "-d",
			present:  true,
		},
		{
			name:     "description absent",
			opts:     CreateOpts{},
			wantFlag: "-d",
			present:  false,
		},
		{
			name:     "type present",
			opts:     CreateOpts{Type: "bug"},
			wantFlag: "-t",
			present:  true,
		},
		{
			name:     "type absent",
			opts:     CreateOpts{},
			wantFlag: "-t",
			present:  false,
		},
		{
			name:     "parent present",
			opts:     CreateOpts{Parent: "beads-001"},
			wantFlag: "--parent",
			present:  true,
		},
		{
			name:     "parent absent",
			opts:     CreateOpts{},
			wantFlag: "--parent",
			present:  false,
		},
		{
			name:     "assignee present",
			opts:     CreateOpts{Assignee: "bob"},
			wantFlag: "-a",
			present:  true,
		},
		{
			name:     "assignee absent",
			opts:     CreateOpts{},
			wantFlag: "-a",
			present:  false,
		},
		{
			name:     "priority present",
			opts:     CreateOpts{Priority: "3"},
			wantFlag: "-p",
			present:  true,
		},
		{
			name:     "priority absent",
			opts:     CreateOpts{},
			wantFlag: "-p",
			present:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			args := buildCreateArgs("title", tt.opts)
			found := false
			for _, arg := range args {
				if arg == tt.wantFlag {
					found = true
					break
				}
			}
			if found != tt.present {
				t.Errorf("flag %q: found=%v, want present=%v (args: %v)", tt.wantFlag, found, tt.present, args)
			}
		})
	}
}

func TestBuildShowArgs(t *testing.T) {
	t.Parallel()
	args := buildShowArgs("beads-abc123")

	want := []string{"show", "beads-abc123", "--json"}
	if len(args) != len(want) {
		t.Fatalf("expected %d args, got %d: %v", len(want), len(args), args)
	}
	for i, w := range want {
		if args[i] != w {
			t.Errorf("args[%d] = %q, want %q", i, args[i], w)
		}
	}
}

func TestBuildUpdateArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
		opts UpdateOpts
		want []string
	}{
		{
			name: "no options",
			id:   "beads-001",
			opts: UpdateOpts{},
			want: []string{"update", "beads-001"},
		},
		{
			name: "status only",
			id:   "beads-002",
			opts: UpdateOpts{Status: "in_progress"},
			want: []string{"update", "beads-002", "-s", "in_progress"},
		},
		{
			name: "assignee only",
			id:   "beads-003",
			opts: UpdateOpts{Assignee: "alice"},
			want: []string{"update", "beads-003", "-a", "alice"},
		},
		{
			name: "status and assignee",
			id:   "beads-004",
			opts: UpdateOpts{Status: "closed", Assignee: "bob"},
			want: []string{"update", "beads-004", "-s", "closed", "-a", "bob"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			args := buildUpdateArgs(tt.id, tt.opts)
			if len(args) != len(tt.want) {
				t.Fatalf("expected %d args, got %d: %v", len(tt.want), len(args), args)
			}
			for i, w := range tt.want {
				if args[i] != w {
					t.Errorf("args[%d] = %q, want %q", i, args[i], w)
				}
			}
		})
	}
}

func TestBuildCloseArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		id     string
		reason string
		want   []string
	}{
		{
			name:   "no reason",
			id:     "beads-001",
			reason: "",
			want:   []string{"close", "beads-001"},
		},
		{
			name:   "with reason",
			id:     "beads-002",
			reason: "completed successfully",
			want:   []string{"close", "beads-002", "-r", "completed successfully"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			args := buildCloseArgs(tt.id, tt.reason)
			if len(args) != len(tt.want) {
				t.Fatalf("expected %d args, got %d: %v", len(tt.want), len(args), args)
			}
			for i, w := range tt.want {
				if args[i] != w {
					t.Errorf("args[%d] = %q, want %q", i, args[i], w)
				}
			}
		})
	}
}

func TestBuildAddCommentArgs(t *testing.T) {
	t.Parallel()
	args := buildAddCommentArgs("beads-001", "LGTM")

	want := []string{"comments", "add", "beads-001", "LGTM"}
	if len(args) != len(want) {
		t.Fatalf("expected %d args, got %d: %v", len(want), len(args), args)
	}
	for i, w := range want {
		if args[i] != w {
			t.Errorf("args[%d] = %q, want %q", i, args[i], w)
		}
	}
}

func TestBuildCreateArgs_MultipleLabels(t *testing.T) {
	t.Parallel()
	args := buildCreateArgs("task", CreateOpts{
		Labels: []string{"a", "b", "c"},
	})

	// Count -l flags.
	var labels []string
	for i, arg := range args {
		if arg == "-l" && i+1 < len(args) {
			labels = append(labels, args[i+1])
		}
	}
	if len(labels) != 3 {
		t.Fatalf("expected 3 -l flags, got %d: %v", len(labels), labels)
	}

	expected := []string{"a", "b", "c"}
	for i, e := range expected {
		if labels[i] != e {
			t.Errorf("label[%d] = %q, want %q", i, labels[i], e)
		}
	}
}

func TestBuildCreateArgs_AlwaysIncludesSilent(t *testing.T) {
	t.Parallel()
	args := buildCreateArgs("any title", CreateOpts{})

	found := false
	for _, arg := range args {
		if arg == "--silent" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected --silent in args: %v", args)
	}
}

func TestBuildQuickCreateArgs_EmptyLabelsSlice(t *testing.T) {
	t.Parallel()
	args := buildQuickCreateArgs("title", CreateOpts{Labels: []string{}})

	for _, arg := range args {
		if arg == "-l" {
			t.Error("expected no -l flags for empty labels slice")
		}
	}
}

func TestBuildAddCommentArgs_BodyWithSpaces(t *testing.T) {
	t.Parallel()
	body := "this is a multi-word comment"
	args := buildAddCommentArgs("beads-001", body)

	if args[3] != body {
		t.Errorf("body arg = %q, want %q", args[3], body)
	}
	// Body should be a single argument, not split on spaces.
	if len(args) != 4 {
		t.Errorf("expected 4 args (body as single arg), got %d: %v", len(args), args)
	}
}

func TestBuildShowArgs_IDPassedThrough(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
	}{
		{name: "standard ID", id: "beads-abc123"},
		{name: "short ID", id: "b-1"},
		{name: "hyphenated ID", id: "beads-abc-def-123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			args := buildShowArgs(tt.id)
			if args[1] != tt.id {
				t.Errorf("ID arg = %q, want %q", args[1], tt.id)
			}
			if !strings.Contains(args[2], "--json") {
				t.Errorf("expected --json flag, got %q", args[2])
			}
		})
	}
}

// --- Execution-path tests using injectable runner ---

// fakeRunner returns a runFunc that records the args it was called with
// and returns the given output or error.
func fakeRunner(wantOutput string, wantErr error, capture *[]string) runFunc {
	return func(_ context.Context, args ...string) (string, error) {
		if capture != nil {
			*capture = args
		}
		return wantOutput, wantErr
	}
}

func TestQuickCreate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		title    string
		opts     CreateOpts
		fakeOut  string
		fakeErr  error
		wantID   string
		wantErr  bool
		wantArgs []string
	}{
		{
			name:     "success returns bead ID",
			title:    "fix bug",
			opts:     CreateOpts{Type: "bug", Priority: "1"},
			fakeOut:  "beads-abc123",
			wantID:   "beads-abc123",
			wantArgs: []string{"q", "fix bug", "-t", "bug", "-p", "1"},
		},
		{
			name:    "run error propagated",
			title:   "failing task",
			opts:    CreateOpts{},
			fakeErr: errors.New("command failed"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var captured []string
			cli := &CLI{
				BeadsPath: "bd",
				runner:    fakeRunner(tt.fakeOut, tt.fakeErr, &captured),
			}

			id, err := cli.QuickCreate(context.Background(), tt.title, tt.opts)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id != tt.wantID {
				t.Errorf("id = %q, want %q", id, tt.wantID)
			}
			if tt.wantArgs != nil {
				assertArgsEqual(t, captured, tt.wantArgs)
			}
		})
	}
}

func TestCreate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		title    string
		opts     CreateOpts
		fakeOut  string
		fakeErr  error
		wantID   string
		wantErr  bool
		wantArgs []string
	}{
		{
			name:    "success with all opts",
			title:   "new feature",
			opts:    CreateOpts{Description: "desc", Type: "feature", Assignee: "alice", Priority: "0"},
			fakeOut: "beads-xyz",
			wantID:  "beads-xyz",
			wantArgs: []string{
				"create", "new feature", "--silent",
				"-d", "desc", "-t", "feature", "-a", "alice", "-p", "0",
			},
		},
		{
			name:     "success minimal opts",
			title:    "minimal",
			opts:     CreateOpts{},
			fakeOut:  "beads-min",
			wantID:   "beads-min",
			wantArgs: []string{"create", "minimal", "--silent"},
		},
		{
			name:    "error propagated",
			title:   "bad",
			opts:    CreateOpts{},
			fakeErr: errors.New("boom"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var captured []string
			cli := &CLI{
				BeadsPath: "bd",
				runner:    fakeRunner(tt.fakeOut, tt.fakeErr, &captured),
			}

			id, err := cli.Create(context.Background(), tt.title, tt.opts)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id != tt.wantID {
				t.Errorf("id = %q, want %q", id, tt.wantID)
			}
			if tt.wantArgs != nil {
				assertArgsEqual(t, captured, tt.wantArgs)
			}
		})
	}
}

func TestShow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		id       string
		fakeOut  string
		fakeErr  error
		wantBead *Bead
		wantErr  string
	}{
		{
			name: "success parses JSON",
			id:   "beads-001",
			fakeOut: `[{"id":"beads-001","title":"fix bug","description":"desc",` +
				`"status":"open","priority":1,"issue_type":"bug","assignee":"alice",` +
				`"labels":["urgent"]}]`,
			wantBead: &Bead{
				ID:          "beads-001",
				Title:       "fix bug",
				Description: "desc",
				Status:      "open",
				Priority:    1,
				IssueType:   "bug",
				Assignee:    "alice",
				Labels:      []string{"urgent"},
			},
		},
		{
			name:    "run error propagated",
			id:      "beads-002",
			fakeErr: errors.New("not found"),
			wantErr: "not found",
		},
		{
			name:    "invalid JSON returns parse error",
			id:      "beads-003",
			fakeOut: "not json",
			wantErr: "failed to parse beads JSON",
		},
		{
			name:    "empty array returns not found",
			id:      "beads-004",
			fakeOut: "[]",
			wantErr: "bead beads-004 not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var captured []string
			cli := &CLI{
				BeadsPath: "bd",
				runner:    fakeRunner(tt.fakeOut, tt.fakeErr, &captured),
			}

			bead, err := cli.Show(context.Background(), tt.id)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if bead.ID != tt.wantBead.ID {
				t.Errorf("bead.ID = %q, want %q", bead.ID, tt.wantBead.ID)
			}
			if bead.Title != tt.wantBead.Title {
				t.Errorf("bead.Title = %q, want %q", bead.Title, tt.wantBead.Title)
			}
			if bead.Status != tt.wantBead.Status {
				t.Errorf("bead.Status = %q, want %q", bead.Status, tt.wantBead.Status)
			}
			if bead.Priority != tt.wantBead.Priority {
				t.Errorf("bead.Priority = %d, want %d", bead.Priority, tt.wantBead.Priority)
			}
			if bead.IssueType != tt.wantBead.IssueType {
				t.Errorf("bead.IssueType = %q, want %q", bead.IssueType, tt.wantBead.IssueType)
			}
			if bead.Assignee != tt.wantBead.Assignee {
				t.Errorf("bead.Assignee = %q, want %q", bead.Assignee, tt.wantBead.Assignee)
			}
			// Verify the correct args were passed to the runner.
			wantArgs := []string{"show", tt.id, "--json"}
			assertArgsEqual(t, captured, wantArgs)
		})
	}
}

func TestUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		id       string
		opts     UpdateOpts
		fakeErr  error
		wantErr  bool
		wantArgs []string
	}{
		{
			name:     "success with status",
			id:       "beads-001",
			opts:     UpdateOpts{Status: "in_progress"},
			wantArgs: []string{"update", "beads-001", "-s", "in_progress"},
		},
		{
			name:     "success with assignee",
			id:       "beads-002",
			opts:     UpdateOpts{Assignee: "bob"},
			wantArgs: []string{"update", "beads-002", "-a", "bob"},
		},
		{
			name:     "success with both",
			id:       "beads-003",
			opts:     UpdateOpts{Status: "closed", Assignee: "alice"},
			wantArgs: []string{"update", "beads-003", "-s", "closed", "-a", "alice"},
		},
		{
			name:    "error propagated",
			id:      "beads-004",
			opts:    UpdateOpts{Status: "x"},
			fakeErr: errors.New("update failed"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var captured []string
			cli := &CLI{
				BeadsPath: "bd",
				runner:    fakeRunner("", tt.fakeErr, &captured),
			}

			err := cli.Update(context.Background(), tt.id, tt.opts)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantArgs != nil {
				assertArgsEqual(t, captured, tt.wantArgs)
			}
		})
	}
}

func TestClose(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		id       string
		reason   string
		fakeErr  error
		wantErr  bool
		wantArgs []string
	}{
		{
			name:     "success without reason",
			id:       "beads-001",
			reason:   "",
			wantArgs: []string{"close", "beads-001"},
		},
		{
			name:     "success with reason",
			id:       "beads-002",
			reason:   "done",
			wantArgs: []string{"close", "beads-002", "-r", "done"},
		},
		{
			name:    "error propagated",
			id:      "beads-003",
			reason:  "",
			fakeErr: errors.New("close failed"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var captured []string
			cli := &CLI{
				BeadsPath: "bd",
				runner:    fakeRunner("", tt.fakeErr, &captured),
			}

			err := cli.Close(context.Background(), tt.id, tt.reason)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantArgs != nil {
				assertArgsEqual(t, captured, tt.wantArgs)
			}
		})
	}
}

func TestAddComment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		id       string
		body     string
		fakeErr  error
		wantErr  bool
		wantArgs []string
	}{
		{
			name:     "success",
			id:       "beads-001",
			body:     "LGTM",
			wantArgs: []string{"comments", "add", "beads-001", "LGTM"},
		},
		{
			name:     "body with spaces",
			id:       "beads-002",
			body:     "this is a multi-word comment",
			wantArgs: []string{"comments", "add", "beads-002", "this is a multi-word comment"},
		},
		{
			name:    "error propagated",
			id:      "beads-003",
			body:    "comment",
			fakeErr: errors.New("comment failed"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var captured []string
			cli := &CLI{
				BeadsPath: "bd",
				runner:    fakeRunner("", tt.fakeErr, &captured),
			}

			err := cli.AddComment(context.Background(), tt.id, tt.body)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantArgs != nil {
				assertArgsEqual(t, captured, tt.wantArgs)
			}
		})
	}
}

func TestRun_VerboseNoRunner(t *testing.T) {
	t.Parallel()

	// Test that run with a non-existent binary returns an error.
	cli := &CLI{
		BeadsPath: "/nonexistent/binary",
		Verbose:   true,
	}
	_, err := cli.run(context.Background(), "version")
	if err == nil {
		t.Fatal("expected error for non-existent binary, got nil")
	}
	if !strings.Contains(err.Error(), "beads command failed") {
		t.Errorf("error = %q, want containing %q", err.Error(), "beads command failed")
	}
}

func TestRun_RunnerOverridesExec(t *testing.T) {
	t.Parallel()

	called := false
	cli := &CLI{
		BeadsPath: "/nonexistent/binary",
		runner: func(_ context.Context, args ...string) (string, error) {
			called = true
			return "mocked", nil
		},
	}
	out, err := cli.run(context.Background(), "anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected runner to be called")
	}
	if out != "mocked" {
		t.Errorf("output = %q, want %q", out, "mocked")
	}
}

func TestShow_MultipleBeadsReturnsFirst(t *testing.T) {
	t.Parallel()

	jsonOut := `[{"id":"beads-001","title":"first","description":"","status":"open","priority":0,"issue_type":"","assignee":"","labels":null},` +
		`{"id":"beads-002","title":"second","description":"","status":"open","priority":0,"issue_type":"","assignee":"","labels":null}]`

	cli := &CLI{
		BeadsPath: "bd",
		runner:    fakeRunner(jsonOut, nil, nil),
	}

	bead, err := cli.Show(context.Background(), "beads-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bead.ID != "beads-001" {
		t.Errorf("bead.ID = %q, want %q", bead.ID, "beads-001")
	}
	if bead.Title != "first" {
		t.Errorf("bead.Title = %q, want %q", bead.Title, "first")
	}
}

// assertArgsEqual is a test helper that compares two string slices.
func assertArgsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("args length = %d, want %d\n  got:  %v\n  want: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

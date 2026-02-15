package beads

import (
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

package gitops

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeRunner records git invocations and replies via an optional fn.
type fakeRunner struct {
	calls [][]string
	fn    func(args []string) (stdout, stderr []byte, err error)
}

func (f *fakeRunner) run(_ context.Context, args ...string) ([]byte, []byte, error) {
	f.calls = append(f.calls, args)
	if f.fn != nil {
		return f.fn(args)
	}
	return nil, nil, nil
}

// lastCall returns the most recent recorded git invocation.
func (f *fakeRunner) lastCall() []string {
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

func TestIsQuasarBranch(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"quasar/foo":          true,
		"quasar/issue-123":    true,
		"quasar/feat/sub.dir": true,
		"quasar/main":         true, // prefixed: allowed by the namespace rule
		"main":                false,
		"master":              false,
		"feature/auth":        false,
		"quasar":              false, // needs a sub-path
		"quasar/":             false,
		"":                    false,
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := IsQuasarBranch(name); got != want {
				t.Errorf("IsQuasarBranch(%q) = %v, want %v", name, got, want)
			}
		})
	}
}

func TestPush(t *testing.T) {
	t.Parallel()

	t.Run("quasar branch force-pushes with lease", func(t *testing.T) {
		t.Parallel()
		fr := &fakeRunner{}
		c := NewWithRunner(".", fr.run)
		if err := c.Push(context.Background(), "quasar/foo"); err != nil {
			t.Fatalf("Push returned error: %v", err)
		}
		got := strings.Join(fr.lastCall(), " ")
		want := "push origin quasar/foo --force-with-lease"
		if got != want {
			t.Errorf("git invocation = %q, want %q", got, want)
		}
	})

	t.Run("refs/heads prefix is stripped", func(t *testing.T) {
		t.Parallel()
		fr := &fakeRunner{}
		c := NewWithRunner(".", fr.run)
		if err := c.Push(context.Background(), "refs/heads/quasar/foo"); err != nil {
			t.Fatalf("Push returned error: %v", err)
		}
		if got := strings.Join(fr.lastCall(), " "); !strings.Contains(got, "origin quasar/foo ") {
			t.Errorf("git invocation = %q, want normalized branch quasar/foo", got)
		}
	})

	unsafe := []string{"main", "master", "feature/auth", "develop", ""}
	for _, branch := range unsafe {
		t.Run("unsafe ref "+branch, func(t *testing.T) {
			t.Parallel()
			fr := &fakeRunner{}
			c := NewWithRunner(".", fr.run)
			err := c.Push(context.Background(), branch)
			if !errors.Is(err, ErrUnsafeRef) {
				t.Fatalf("Push(%q) error = %v, want ErrUnsafeRef", branch, err)
			}
			if len(fr.calls) != 0 {
				t.Errorf("runner invoked %d times for unsafe ref; want 0", len(fr.calls))
			}
		})
	}

	t.Run("quasar/main is allowed", func(t *testing.T) {
		t.Parallel()
		fr := &fakeRunner{}
		c := NewWithRunner(".", fr.run)
		if err := c.Push(context.Background(), "quasar/main"); err != nil {
			t.Fatalf("Push(quasar/main) error = %v, want nil", err)
		}
		if len(fr.calls) != 1 {
			t.Errorf("runner invoked %d times; want 1", len(fr.calls))
		}
	})

	t.Run("non-fast-forward surfaces ErrForcePushRejected", func(t *testing.T) {
		t.Parallel()
		fr := &fakeRunner{fn: func(args []string) ([]byte, []byte, error) {
			return nil, []byte("! [rejected] quasar/foo -> quasar/foo (non-fast-forward)"), errors.New("exit status 1")
		}}
		c := NewWithRunner(".", fr.run)
		err := c.Push(context.Background(), "quasar/foo")
		if !errors.Is(err, ErrForcePushRejected) {
			t.Fatalf("Push error = %v, want ErrForcePushRejected", err)
		}
	})

	t.Run("other push failure is a generic error", func(t *testing.T) {
		t.Parallel()
		fr := &fakeRunner{fn: func(args []string) ([]byte, []byte, error) {
			return nil, []byte("fatal: Authentication failed"), errors.New("exit status 128")
		}}
		c := NewWithRunner(".", fr.run)
		err := c.Push(context.Background(), "quasar/foo")
		if err == nil || errors.Is(err, ErrForcePushRejected) {
			t.Fatalf("Push error = %v, want a generic (non-rejection) error", err)
		}
	})
}

func TestCreateBranch(t *testing.T) {
	t.Parallel()

	t.Run("quasar branch", func(t *testing.T) {
		t.Parallel()
		fr := &fakeRunner{}
		c := NewWithRunner(".", fr.run)
		if err := c.CreateBranch(context.Background(), "quasar/foo"); err != nil {
			t.Fatalf("CreateBranch error: %v", err)
		}
		if got := strings.Join(fr.lastCall(), " "); got != "branch quasar/foo" {
			t.Errorf("git invocation = %q, want %q", got, "branch quasar/foo")
		}
	})

	t.Run("non-quasar branch refused", func(t *testing.T) {
		t.Parallel()
		fr := &fakeRunner{}
		c := NewWithRunner(".", fr.run)
		err := c.CreateBranch(context.Background(), "main")
		if !errors.Is(err, ErrUnsafeRef) {
			t.Fatalf("CreateBranch(main) error = %v, want ErrUnsafeRef", err)
		}
		if len(fr.calls) != 0 {
			t.Errorf("runner invoked for unsafe branch creation")
		}
	})
}

func TestStatusAndHead(t *testing.T) {
	t.Parallel()

	t.Run("clean worktree", func(t *testing.T) {
		t.Parallel()
		fr := &fakeRunner{fn: func(args []string) ([]byte, []byte, error) {
			return []byte("\n"), nil, nil
		}}
		c := NewWithRunner(".", fr.run)
		clean, err := c.Status(context.Background())
		if err != nil {
			t.Fatalf("Status error: %v", err)
		}
		if !clean {
			t.Error("Status clean = false, want true for empty porcelain output")
		}
	})

	t.Run("dirty worktree", func(t *testing.T) {
		t.Parallel()
		fr := &fakeRunner{fn: func(args []string) ([]byte, []byte, error) {
			return []byte(" M file.go\n"), nil, nil
		}}
		c := NewWithRunner(".", fr.run)
		clean, err := c.Status(context.Background())
		if err != nil {
			t.Fatalf("Status error: %v", err)
		}
		if clean {
			t.Error("Status clean = true, want false for non-empty porcelain output")
		}
	})

	t.Run("head sha trimmed", func(t *testing.T) {
		t.Parallel()
		fr := &fakeRunner{fn: func(args []string) ([]byte, []byte, error) {
			return []byte("abc123\n"), nil, nil
		}}
		c := NewWithRunner(".", fr.run)
		sha, err := c.HeadSHA(context.Background())
		if err != nil {
			t.Fatalf("HeadSHA error: %v", err)
		}
		if sha != "abc123" {
			t.Errorf("HeadSHA = %q, want abc123", sha)
		}
	})
}

func TestLog(t *testing.T) {
	t.Parallel()

	out := "sha1" + logFieldSep + "Ada" + logFieldSep + "first" + logRecordSep +
		"\nsha2" + logFieldSep + "Linus" + logFieldSep + "second" + logRecordSep
	fr := &fakeRunner{fn: func(args []string) ([]byte, []byte, error) {
		return []byte(out), nil, nil
	}}
	c := NewWithRunner(".", fr.run)

	commits, err := c.Log(context.Background(), LogOpts{MaxCount: 2, Range: "base..head"})
	if err != nil {
		t.Fatalf("Log error: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("Log returned %d commits, want 2", len(commits))
	}
	if commits[0] != (CommitInfo{SHA: "sha1", Author: "Ada", Subject: "first"}) {
		t.Errorf("commits[0] = %+v", commits[0])
	}
	got := strings.Join(fr.lastCall(), " ")
	if !strings.Contains(got, "-n2") || !strings.Contains(got, "base..head") {
		t.Errorf("git log invocation = %q, want -n2 and range", got)
	}
}

package gitops

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// commitRunner simulates the git calls Commit makes: add -u, diff --cached
// --quiet, commit, and rev-parse HEAD. indexEmpty controls whether the staged
// index is reported empty.
func commitRunner(indexEmpty bool) *fakeRunner {
	return &fakeRunner{fn: func(args []string) ([]byte, []byte, error) {
		switch {
		case len(args) >= 3 && args[0] == "diff" && args[1] == "--cached":
			// Exit 0 (nil err) means nothing staged; non-nil means changes.
			if indexEmpty {
				return nil, nil, nil
			}
			return nil, nil, errors.New("exit status 1")
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "HEAD":
			return []byte("newsha\n"), nil, nil
		default:
			return nil, nil, nil
		}
	}}
}

// recordShell returns a shell runner that records commands and replies per cmd.
func recordShell(recorded *[]string, fail map[string]bool) shellRunner {
	return func(_ context.Context, _ string, command string) ([]byte, []byte, int, error) {
		*recorded = append(*recorded, command)
		if fail[command] {
			return nil, []byte("boom"), 1, errors.New("exit status 1")
		}
		return nil, nil, 0, nil
	}
}

func TestCommit(t *testing.T) {
	t.Parallel()

	t.Run("runs pre-commit before committing", func(t *testing.T) {
		t.Parallel()
		fr := commitRunner(false)
		c := NewWithRunner(".", fr.run)
		var ran []string
		c.runShell = recordShell(&ran, nil)

		sha, err := c.Commit(context.Background(), "msg", CommitOpts{
			PreCommit: PreCommitConfig{Commands: []string{"gofmt -w ."}, FailOnError: true},
		})
		if err != nil {
			t.Fatalf("Commit error: %v", err)
		}
		if sha != "newsha" {
			t.Errorf("Commit sha = %q, want newsha", sha)
		}
		if len(ran) != 1 || ran[0] != "gofmt -w ." {
			t.Errorf("pre-commit commands ran = %v, want [gofmt -w .]", ran)
		}
		// git commit must have been invoked.
		sawCommit := false
		for _, call := range fr.calls {
			if len(call) > 0 && call[0] == "commit" {
				sawCommit = true
			}
		}
		if !sawCommit {
			t.Error("git commit was not invoked")
		}
	})

	t.Run("failing pre-commit with FailOnError aborts before commit", func(t *testing.T) {
		t.Parallel()
		fr := commitRunner(false)
		c := NewWithRunner(".", fr.run)
		var ran []string
		c.runShell = recordShell(&ran, map[string]bool{"false": true})

		_, err := c.Commit(context.Background(), "msg", CommitOpts{
			PreCommit: PreCommitConfig{Commands: []string{"false"}, FailOnError: true},
		})
		if !errors.Is(err, ErrPreCommitFailed) {
			t.Fatalf("Commit error = %v, want ErrPreCommitFailed", err)
		}
		for _, call := range fr.calls {
			if len(call) > 0 && call[0] == "commit" {
				t.Error("git commit was invoked despite pre-commit failure")
			}
		}
	})

	t.Run("failing pre-commit without FailOnError proceeds to commit", func(t *testing.T) {
		t.Parallel()
		fr := commitRunner(false)
		c := NewWithRunner(".", fr.run)
		var ran []string
		c.runShell = recordShell(&ran, map[string]bool{"false": true})

		sha, err := c.Commit(context.Background(), "msg", CommitOpts{
			PreCommit: PreCommitConfig{Commands: []string{"false"}, FailOnError: false},
		})
		if err != nil {
			t.Fatalf("Commit error = %v, want nil", err)
		}
		if sha != "newsha" {
			t.Errorf("Commit sha = %q, want newsha", sha)
		}
	})

	t.Run("empty index returns ErrNothingToCommit", func(t *testing.T) {
		t.Parallel()
		fr := commitRunner(true)
		c := NewWithRunner(".", fr.run)

		_, err := c.Commit(context.Background(), "msg", CommitOpts{})
		if !errors.Is(err, ErrNothingToCommit) {
			t.Fatalf("Commit error = %v, want ErrNothingToCommit", err)
		}
		for _, call := range fr.calls {
			if len(call) > 0 && call[0] == "commit" {
				t.Error("git commit was invoked with an empty index")
			}
		}
	})

	t.Run("author flag passed through", func(t *testing.T) {
		t.Parallel()
		fr := commitRunner(false)
		c := NewWithRunner(".", fr.run)

		if _, err := c.Commit(context.Background(), "msg", CommitOpts{
			Author: "Quasar <quasar@noreply.local>",
		}); err != nil {
			t.Fatalf("Commit error: %v", err)
		}
		var commitCall string
		for _, call := range fr.calls {
			if len(call) > 0 && call[0] == "commit" {
				commitCall = strings.Join(call, " ")
			}
		}
		if !strings.Contains(commitCall, "--author Quasar <quasar@noreply.local>") {
			t.Errorf("commit call = %q, want --author flag", commitCall)
		}
	})
}

func TestRunPreCommit(t *testing.T) {
	t.Parallel()

	t.Run("FailOnError false collects all results", func(t *testing.T) {
		t.Parallel()
		c := NewWithRunner(".", (&fakeRunner{}).run)
		var ran []string
		c.runShell = recordShell(&ran, map[string]bool{"bad": true})

		results, err := c.RunPreCommit(context.Background(), PreCommitConfig{
			Commands:    []string{"good", "bad", "good2"},
			FailOnError: false,
		})
		if err != nil {
			t.Fatalf("RunPreCommit error = %v, want nil", err)
		}
		if len(results) != 3 {
			t.Fatalf("results = %d, want 3", len(results))
		}
		if results[1].ExitCode != 1 || results[1].Err == nil {
			t.Errorf("results[1] = %+v, want non-zero exit with error", results[1])
		}
	})

	t.Run("FailOnError true stops at first failure", func(t *testing.T) {
		t.Parallel()
		c := NewWithRunner(".", (&fakeRunner{}).run)
		var ran []string
		c.runShell = recordShell(&ran, map[string]bool{"bad": true})

		results, err := c.RunPreCommit(context.Background(), PreCommitConfig{
			Commands:    []string{"good", "bad", "never"},
			FailOnError: true,
		})
		if !errors.Is(err, ErrPreCommitFailed) {
			t.Fatalf("error = %v, want ErrPreCommitFailed", err)
		}
		if len(results) != 2 {
			t.Fatalf("results = %d, want 2 (stopped at failure)", len(results))
		}
		if ran[len(ran)-1] != "bad" {
			t.Errorf("last command run = %q, want bad", ran[len(ran)-1])
		}
	})
}

func TestParseRemoteURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		url               string
		host, owner, repo string
		wantErr           bool
	}{
		{name: "ssh", url: "git@github.com:papapumpkin/quasar.git", host: "github.com", owner: "papapumpkin", repo: "quasar"},
		{name: "ssh no suffix", url: "git@github.com:papapumpkin/quasar", host: "github.com", owner: "papapumpkin", repo: "quasar"},
		{name: "https", url: "https://github.com/papapumpkin/quasar.git", host: "github.com", owner: "papapumpkin", repo: "quasar"},
		{name: "https with user", url: "https://user@gitlab.com/group/sub/repo.git", host: "gitlab.com", owner: "group/sub", repo: "repo"},
		{name: "empty", url: "", wantErr: true},
		{name: "garbage", url: "not-a-url", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host, owner, repo, err := ParseRemoteURL(tc.url)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseRemoteURL(%q) error = nil, want error", tc.url)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRemoteURL(%q) error: %v", tc.url, err)
			}
			if host != tc.host || owner != tc.owner || repo != tc.repo {
				t.Errorf("ParseRemoteURL(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tc.url, host, owner, repo, tc.host, tc.owner, tc.repo)
			}
		})
	}
}

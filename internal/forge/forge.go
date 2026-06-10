// Package forge is Quasar's write-side adapter to a code host (GitHub today).
// It is the only package outside internal/sensors/github that may shell out
// to gh — the arch test at internal/arch_test/safety_test.go enforces this.
//
// The write surface is intentionally narrow: open a PR. Anything broader
// (merge, comment, close) is deliberately omitted because Quasar's safety
// model forbids those operations.
package forge

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// PROpts configures a single PR-create call.
type PROpts struct {
	// WorkDir is the repo working directory the gh CLI runs in. Required.
	WorkDir string
	// Head is the branch name to PR from (typically a quasar/* branch).
	Head string
	// Base is the branch to PR into (typically main). When empty, gh uses
	// the repo's default branch.
	Base string
	// Title is the PR title. Required; gh rejects empty titles.
	Title string
	// Body is the PR description. Empty body is allowed.
	Body string
}

// PRResult carries the gh-reported PR identifiers a caller might need to
// surface (the URL is what the TUI displays; the number is what later
// `gh pr view` calls look up).
type PRResult struct {
	URL    string
	Number int
}

// OpenPR runs `gh pr create` with the provided options and returns the
// resulting PR's URL and number. It does NOT run `gh pr merge` or any other
// gh subcommand — Quasar's safety model forbids those operations and the
// arch test enforces it.
func OpenPR(ctx context.Context, opts PROpts) (PRResult, error) {
	if strings.TrimSpace(opts.Head) == "" {
		return PRResult{}, fmt.Errorf("forge: OpenPR requires Head branch")
	}
	if strings.TrimSpace(opts.Title) == "" {
		return PRResult{}, fmt.Errorf("forge: OpenPR requires Title")
	}
	if strings.TrimSpace(opts.WorkDir) == "" {
		return PRResult{}, fmt.Errorf("forge: OpenPR requires WorkDir")
	}

	args := []string{"pr", "create",
		"--head", opts.Head,
		"--title", opts.Title,
		"--body", opts.Body,
	}
	if strings.TrimSpace(opts.Base) != "" {
		args = append(args, "--base", opts.Base)
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = opts.WorkDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return PRResult{}, fmt.Errorf("forge: gh pr create: %w: %s",
			err, strings.TrimSpace(stderr.String()))
	}

	url := strings.TrimSpace(stdout.String())
	if url == "" {
		return PRResult{}, fmt.Errorf("forge: gh pr create returned no URL")
	}
	num, _ := parsePRNumber(url)
	return PRResult{URL: url, Number: num}, nil
}

// parsePRNumber extracts the trailing /<number> from a PR URL like
// https://github.com/owner/repo/pull/42. Returns 0 if the trailing segment
// cannot be parsed; the caller treats 0 as "unknown" rather than an error.
func parsePRNumber(url string) (int, bool) {
	url = strings.TrimSpace(url)
	if url == "" {
		return 0, false
	}
	i := strings.LastIndex(url, "/")
	if i < 0 || i == len(url)-1 {
		return 0, false
	}
	var n int
	if _, err := fmt.Sscanf(url[i+1:], "%d", &n); err != nil {
		return 0, false
	}
	return n, n > 0
}

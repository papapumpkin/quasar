package relativity

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// GitQuerier abstracts git operations for testability.
type GitQuerier interface {
	// BranchExists reports whether a local branch with the given name exists.
	BranchExists(ctx context.Context, name string) (bool, error)

	// FirstCommitOnBranch returns the author timestamp of the earliest commit
	// unique to the given branch (i.e. not on main).
	FirstCommitOnBranch(ctx context.Context, branch string) (time.Time, error)

	// FirstCommitTouching returns the author timestamp of the earliest commit
	// that added files under the given path.
	FirstCommitTouching(ctx context.Context, path string) (time.Time, error)

	// MergeCommitToMain returns the timestamp of the merge commit that brought
	// the branch into main. Returns an error if the branch is not merged.
	MergeCommitToMain(ctx context.Context, branch string) (time.Time, error)

	// DiffPackages returns Go package directories that were added or modified
	// between two commits.
	DiffPackages(ctx context.Context, base, head string) (added, modified []string, err error)
}

// CLIGitQuerier implements GitQuerier using git CLI commands.
type CLIGitQuerier struct {
	RepoDir string
}

// run executes a git command and returns its trimmed stdout.
func (g *CLIGitQuerier) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.RepoDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(stderr.String()), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// BranchExists checks whether a local branch with the given name exists.
func (g *CLIGitQuerier) BranchExists(ctx context.Context, name string) (bool, error) {
	_, err := g.run(ctx, "rev-parse", "--verify", "refs/heads/"+name)
	if err != nil {
		return false, nil
	}
	return true, nil
}

// FirstCommitOnBranch returns the timestamp of the first commit unique to the
// given branch (commits on branch but not on main).
func (g *CLIGitQuerier) FirstCommitOnBranch(ctx context.Context, branch string) (time.Time, error) {
	output, err := g.run(ctx, "log", "--reverse", "--format=%aI", "main.."+branch)
	if err != nil {
		return time.Time{}, fmt.Errorf("first commit on %s: %w", branch, err)
	}
	return parseFirstTimestamp(output)
}

// FirstCommitTouching returns the timestamp of the first commit that added
// files under the given path.
func (g *CLIGitQuerier) FirstCommitTouching(ctx context.Context, path string) (time.Time, error) {
	output, err := g.run(ctx, "log", "--reverse", "--format=%aI", "--diff-filter=A", "--", path)
	if err != nil {
		return time.Time{}, fmt.Errorf("first commit touching %s: %w", path, err)
	}
	return parseFirstTimestamp(output)
}

// MergeCommitToMain returns the timestamp of the merge commit that brought the
// branch into main. Returns an error if the branch is not merged.
func (g *CLIGitQuerier) MergeCommitToMain(ctx context.Context, branch string) (time.Time, error) {
	// Check if branch is an ancestor of main (i.e. merged).
	_, err := g.run(ctx, "merge-base", "--is-ancestor", branch, "main")
	if err != nil {
		return time.Time{}, fmt.Errorf("branch %s not merged into main", branch)
	}

	// Find merge commits in the ancestry path.
	output, err := g.run(ctx, "log", "--merges", "--ancestry-path", "--format=%aI", branch+"..main")
	if err != nil {
		return time.Time{}, fmt.Errorf("finding merge commit for %s: %w", branch, err)
	}

	if output == "" {
		// Fast-forward merge: use the branch tip timestamp.
		output, err = g.run(ctx, "log", "-1", "--format=%aI", branch)
		if err != nil {
			return time.Time{}, fmt.Errorf("reading branch tip %s: %w", branch, err)
		}
	}

	// Take the last line (earliest merge commit in the ancestry path).
	lines := strings.Split(output, "\n")
	return time.Parse(time.RFC3339, lines[len(lines)-1])
}

// DiffPackages returns the Go package directories that were added or modified
// between two commits. It extracts paths matching internal/*/ and cmd/ patterns.
func (g *CLIGitQuerier) DiffPackages(ctx context.Context, base, head string) (added, modified []string, err error) {
	output, err := g.run(ctx, "diff", "--name-only", base, head)
	if err != nil {
		return nil, nil, fmt.Errorf("diffing %s..%s: %w", base, head, err)
	}
	if output == "" {
		return nil, nil, nil
	}

	pkgs := extractPackages(strings.Split(output, "\n"))

	for _, pkg := range pkgs {
		// Check if the package existed at base.
		_, checkErr := g.run(ctx, "cat-file", "-e", base+":"+pkg)
		if checkErr != nil {
			added = append(added, pkg)
		} else {
			modified = append(modified, pkg)
		}
	}

	sort.Strings(added)
	sort.Strings(modified)
	return added, modified, nil
}

// parseFirstTimestamp extracts the first RFC3339 timestamp from multi-line
// git log output.
func parseFirstTimestamp(output string) (time.Time, error) {
	if output == "" {
		return time.Time{}, fmt.Errorf("no timestamps in output")
	}
	first := strings.SplitN(output, "\n", 2)[0]
	return time.Parse(time.RFC3339, first)
}

// extractPackages deduplicates and returns Go package directories from a list
// of file paths. It recognizes internal/*/ and cmd/ patterns.
func extractPackages(files []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, f := range files {
		pkg := extractPackage(f)
		if pkg != "" && !seen[pkg] {
			seen[pkg] = true
			result = append(result, pkg)
		}
	}
	sort.Strings(result)
	return result
}

// extractPackage maps a file path to its containing Go package directory.
func extractPackage(filePath string) string {
	parts := strings.Split(filePath, "/")
	if len(parts) == 0 {
		return ""
	}
	if parts[0] == "internal" && len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	if parts[0] == "cmd" {
		return "cmd"
	}
	return ""
}

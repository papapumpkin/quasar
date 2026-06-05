package gitops

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrUnsafeRef indicates an operation targeted a ref outside the quasar/*
// namespace, or a forbidden base branch. Quasar refuses to write to refs it
// does not own; there is no override flag.
var ErrUnsafeRef = errors.New("unsafe git ref: only quasar/* branches may be written")

// ErrForcePushRejected indicates a --force-with-lease push was rejected because
// the remote branch advanced since Quasar last saw it (another agent or a human
// pushed). This is a safe failure: nothing was overwritten.
var ErrForcePushRejected = errors.New("force-with-lease push rejected: remote branch advanced")

// quasarBranchRe matches branch names Quasar is permitted to write: the
// quasar/ prefix followed by one or more path-safe characters.
var quasarBranchRe = regexp.MustCompile(`^quasar/[A-Za-z0-9._/-]+$`)

// forbiddenPushBranches are base-branch names Quasar must never push to, even
// if some upstream bug let them past the quasar/* check. This is defense in
// depth and is intentionally not config-overridable so a misconfiguration can
// never disable the guard.
var forbiddenPushBranches = []string{
	"main", "master", "develop", "trunk",
	"production", "prod", "release", "staging",
}

// IsQuasarBranch reports whether name is in the quasar/* namespace Quasar is
// allowed to write. It is exported so other packages (e.g. a future forge
// adapter checking PR head refs) can reuse the exact allowlist.
func IsQuasarBranch(name string) bool {
	return quasarBranchRe.MatchString(name)
}

// isForbiddenBranch reports whether the normalized branch name exactly matches
// a forbidden base branch.
func isForbiddenBranch(name string) bool {
	for _, f := range forbiddenPushBranches {
		if name == f {
			return true
		}
	}
	return false
}

// Push force-pushes branch to origin using --force-with-lease. The branch must
// be in the quasar/* namespace (see IsQuasarBranch); otherwise Push returns
// ErrUnsafeRef without invoking git. --force-with-lease is intentional: Quasar
// owns and may rewrite quasar/* branches during fix cycles, but the push fails
// safely (ErrForcePushRejected) if the remote advanced underneath it.
func (c *Client) Push(ctx context.Context, branch string) error {
	branch = strings.TrimPrefix(branch, "refs/heads/")

	if !IsQuasarBranch(branch) {
		return fmt.Errorf("%w: %q", ErrUnsafeRef, branch)
	}
	// Defense in depth: reject a bare base-branch name even though the regex
	// above already requires the quasar/ prefix.
	if isForbiddenBranch(branch) {
		return fmt.Errorf("%w: %q is a forbidden base branch", ErrUnsafeRef, branch)
	}

	_, stderr, err := c.runGit(ctx, "push", "origin", branch, "--force-with-lease")
	if err != nil {
		if isForcePushRejection(string(stderr)) {
			return fmt.Errorf("%w: recover with `quasar abandon <id>` or push manually: %s",
				ErrForcePushRejected, strings.TrimSpace(string(stderr)))
		}
		return fmt.Errorf("git push origin %s --force-with-lease: %w: %s",
			branch, err, strings.TrimSpace(string(stderr)))
	}
	return nil
}

// isForcePushRejection classifies git's stderr as a non-fast-forward / stale
// lease rejection (as opposed to an auth, network, or other failure).
func isForcePushRejection(stderr string) bool {
	if strings.Contains(stderr, "stale info") {
		return true
	}
	return strings.Contains(stderr, "rejected") && strings.Contains(stderr, "non-fast-forward")
}

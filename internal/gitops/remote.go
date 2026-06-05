package gitops

import (
	"context"
	"fmt"
	"strings"
)

// OriginURL returns the configured URL of the "origin" remote.
func (c *Client) OriginURL(ctx context.Context) (string, error) {
	return c.run(ctx, "remote", "get-url", "origin")
}

// Fetch fetches ref from origin without merging or modifying the worktree.
func (c *Client) Fetch(ctx context.Context, ref string) error {
	_, err := c.run(ctx, "fetch", "origin", ref)
	return err
}

// ParseRemoteURL extracts the host, owner, and repository from a git remote URL.
// It handles both SSH (git@github.com:owner/repo.git) and HTTPS
// (https://github.com/owner/repo.git) forms, with or without a trailing ".git".
func ParseRemoteURL(url string) (host, owner, repo string, err error) {
	raw := strings.TrimSpace(url)
	if raw == "" {
		return "", "", "", fmt.Errorf("empty remote URL")
	}

	var hostAndPath string
	switch {
	case strings.HasPrefix(raw, "git@"):
		// scp-like syntax: git@host:owner/repo.git
		trimmed := strings.TrimPrefix(raw, "git@")
		h, path, ok := strings.Cut(trimmed, ":")
		if !ok {
			return "", "", "", fmt.Errorf("malformed ssh remote URL %q", url)
		}
		host = h
		hostAndPath = path
	case strings.Contains(raw, "://"):
		// scheme://[user@]host/owner/repo.git
		_, rest, _ := strings.Cut(raw, "://")
		if at := strings.LastIndex(rest, "@"); at != -1 {
			rest = rest[at+1:]
		}
		h, path, ok := strings.Cut(rest, "/")
		if !ok {
			return "", "", "", fmt.Errorf("malformed remote URL %q", url)
		}
		host = h
		hostAndPath = path
	default:
		return "", "", "", fmt.Errorf("unrecognized remote URL form %q", url)
	}

	path := strings.TrimSuffix(strings.Trim(hostAndPath, "/"), ".git")
	owner, repo, ok := splitOwnerRepo(path)
	if !ok {
		return "", "", "", fmt.Errorf("cannot derive owner/repo from %q", url)
	}
	return host, owner, repo, nil
}

// splitOwnerRepo separates "owner/repo" (allowing nested groups such as
// "group/subgroup/repo", where everything before the final segment is the
// owner). It returns ok=false if either component is empty.
func splitOwnerRepo(path string) (owner, repo string, ok bool) {
	idx := strings.LastIndex(path, "/")
	if idx <= 0 || idx == len(path)-1 {
		return "", "", false
	}
	return path[:idx], path[idx+1:], true
}

package beads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CLI implements Client by shelling out to the beads CLI.
type CLI struct {
	BeadsPath string
	Verbose   bool
}

func (c *CLI) run(ctx context.Context, args ...string) (string, error) {
	if c.Verbose {
		fmt.Fprintf(os.Stderr, "[beads] running: %s %s\n", c.BeadsPath, strings.Join(args, " "))
	}

	cmd := exec.CommandContext(ctx, c.BeadsPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("beads command failed: %w\nstderr: %s", err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// buildQuickCreateArgs constructs CLI arguments for the quick-create command.
func buildQuickCreateArgs(title string, opts CreateOpts) []string {
	args := []string{"q", title}

	if opts.Type != "" {
		args = append(args, "-t", opts.Type)
	}
	for _, l := range opts.Labels {
		args = append(args, "-l", l)
	}
	if opts.Priority != "" {
		args = append(args, "-p", opts.Priority)
	}
	return args
}

// QuickCreate creates a bead using the quick-create command and returns its ID.
func (c *CLI) QuickCreate(ctx context.Context, title string, opts CreateOpts) (string, error) {
	args := buildQuickCreateArgs(title, opts)

	out, err := c.run(ctx, args...)
	if err != nil {
		return "", err
	}
	// `beads q` outputs the bead ID directly.
	return out, nil
}

// buildCreateArgs constructs CLI arguments for the create command.
func buildCreateArgs(title string, opts CreateOpts) []string {
	args := []string{"create", title, "--silent"}

	if opts.Description != "" {
		args = append(args, "-d", opts.Description)
	}
	if opts.Type != "" {
		args = append(args, "-t", opts.Type)
	}
	for _, l := range opts.Labels {
		args = append(args, "-l", l)
	}
	if opts.Parent != "" {
		args = append(args, "--parent", opts.Parent)
	}
	if opts.Assignee != "" {
		args = append(args, "-a", opts.Assignee)
	}
	if opts.Priority != "" {
		args = append(args, "-p", opts.Priority)
	}
	return args
}

// Create creates a bead with full options and returns its ID.
func (c *CLI) Create(ctx context.Context, title string, opts CreateOpts) (string, error) {
	args := buildCreateArgs(title, opts)

	out, err := c.run(ctx, args...)
	if err != nil {
		return "", err
	}
	return out, nil
}

// buildShowArgs constructs CLI arguments for the show command.
func buildShowArgs(id string) []string {
	return []string{"show", id, "--json"}
}

// Show retrieves a bead by ID.
func (c *CLI) Show(ctx context.Context, id string) (*Bead, error) {
	out, err := c.run(ctx, buildShowArgs(id)...)
	if err != nil {
		return nil, err
	}

	var beads []Bead
	if err := json.Unmarshal([]byte(out), &beads); err != nil {
		return nil, fmt.Errorf("failed to parse beads JSON: %w", err)
	}
	if len(beads) == 0 {
		return nil, fmt.Errorf("bead %s not found", id)
	}
	return &beads[0], nil
}

// buildUpdateArgs constructs CLI arguments for the update command.
func buildUpdateArgs(id string, opts UpdateOpts) []string {
	args := []string{"update", id}

	if opts.Status != "" {
		args = append(args, "-s", opts.Status)
	}
	if opts.Assignee != "" {
		args = append(args, "-a", opts.Assignee)
	}
	return args
}

// Update modifies a bead's status and/or assignee.
func (c *CLI) Update(ctx context.Context, id string, opts UpdateOpts) error {
	_, err := c.run(ctx, buildUpdateArgs(id, opts)...)
	return err
}

// buildCloseArgs constructs CLI arguments for the close command.
func buildCloseArgs(id string, reason string) []string {
	args := []string{"close", id}

	if reason != "" {
		args = append(args, "-r", reason)
	}
	return args
}

// Close closes a bead with an optional reason.
func (c *CLI) Close(ctx context.Context, id string, reason string) error {
	_, err := c.run(ctx, buildCloseArgs(id, reason)...)
	return err
}

// buildAddCommentArgs constructs CLI arguments for the add-comment command.
func buildAddCommentArgs(id string, body string) []string {
	return []string{"comments", "add", id, body}
}

// AddComment adds a comment to a bead.
func (c *CLI) AddComment(ctx context.Context, id string, body string) error {
	_, err := c.run(ctx, buildAddCommentArgs(id, body)...)
	return err
}

// Validate checks that the beads CLI binary is available.
func (c *CLI) Validate() error {
	cmd := exec.Command(c.BeadsPath, "--version")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("beads CLI not found at %q: %w", c.BeadsPath, err)
	}
	if c.Verbose {
		fmt.Fprintf(os.Stderr, "[beads] version: %s", string(out))
	}
	return nil
}

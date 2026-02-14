package beads

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Client struct {
	BeadsPath string
	Verbose   bool
}

func (c *Client) run(args ...string) (string, error) {
	if c.Verbose {
		fmt.Fprintf(os.Stderr, "[beads] running: %s %s\n", c.BeadsPath, strings.Join(args, " "))
	}

	cmd := exec.Command(c.BeadsPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("beads command failed: %w\nstderr: %s", err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// QuickCreate creates a bead using the quick-create command and returns its ID.
func (c *Client) QuickCreate(title string, opts CreateOpts) (string, error) {
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

	out, err := c.run(args...)
	if err != nil {
		return "", err
	}
	// `beads q` outputs the bead ID directly.
	return out, nil
}

// Create creates a bead with full options and returns its ID.
func (c *Client) Create(title string, opts CreateOpts) (string, error) {
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

	out, err := c.run(args...)
	if err != nil {
		return "", err
	}
	return out, nil
}

// Show retrieves a bead by ID.
func (c *Client) Show(id string) (*Bead, error) {
	out, err := c.run("show", id, "--json")
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

// Update modifies a bead's status and/or assignee.
func (c *Client) Update(id string, opts UpdateOpts) error {
	args := []string{"update", id}

	if opts.Status != "" {
		args = append(args, "-s", opts.Status)
	}
	if opts.Assignee != "" {
		args = append(args, "-a", opts.Assignee)
	}

	_, err := c.run(args...)
	return err
}

// Close closes a bead with an optional reason.
func (c *Client) Close(id string, reason string) error {
	args := []string{"close", id}

	if reason != "" {
		args = append(args, "-r", reason)
	}

	_, err := c.run(args...)
	return err
}

// AddComment adds a comment to a bead.
func (c *Client) AddComment(id string, body string) error {
	_, err := c.run("comments", "add", id, body)
	return err
}

// Validate checks that the beads CLI binary is available.
func (c *Client) Validate() error {
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

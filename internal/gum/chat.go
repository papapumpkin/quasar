package gum

import (
	"fmt"
	"os/exec"
	"strings"
)

// PhaseContext holds execution state displayed to the developer before
// collecting guidance input. It provides situational awareness so the
// developer can craft relevant instructions.
type PhaseContext struct {
	PhaseID        string
	PhaseTitle     string
	Cycle          int
	MaxCycles      int
	ActiveAgent    string   // "coder" or "reviewer" (empty if idle)
	LastFeedback   string   // last reviewer summary (truncated)
	FileClaims     []string // files currently claimed by the phase
	RecentActivity string   // latest worker activity line
}

// buildContextMarkdown constructs a markdown summary of the phase context
// for display via gum format before the guidance prompt.
func buildContextMarkdown(ctx PhaseContext) string {
	var md strings.Builder
	md.WriteString(fmt.Sprintf("# Phase: %s\n\n", ctx.PhaseID))

	if ctx.PhaseTitle != "" {
		md.WriteString(fmt.Sprintf("**Title:** %s\n", ctx.PhaseTitle))
	}

	if ctx.MaxCycles > 0 {
		md.WriteString(fmt.Sprintf("**Cycle:** %d/%d", ctx.Cycle, ctx.MaxCycles))
	} else if ctx.Cycle > 0 {
		md.WriteString(fmt.Sprintf("**Cycle:** %d", ctx.Cycle))
	}

	if ctx.ActiveAgent != "" {
		md.WriteString(fmt.Sprintf(" · **%s** active", ctx.ActiveAgent))
	}
	md.WriteString("\n")

	if ctx.LastFeedback != "" {
		md.WriteString(fmt.Sprintf("\n**Last reviewer feedback:**\n%s\n", ctx.LastFeedback))
	}

	if len(ctx.FileClaims) > 0 {
		md.WriteString("\n**Files claimed:**\n")
		for _, f := range ctx.FileClaims {
			md.WriteString(fmt.Sprintf("- `%s`\n", f))
		}
	}

	if ctx.RecentActivity != "" {
		md.WriteString(fmt.Sprintf("\n**Recent:** %s\n", ctx.RecentActivity))
	}

	return md.String()
}

// GumGuidanceWriteCmd builds an *exec.Cmd that displays phase context via
// gum format and collects multi-line guidance via gum write. Designed for
// use with tea.ExecProcess to suspend the TUI during interaction.
//
// The command runs a shell script that:
// 1. Displays phase context via gum format (to stderr)
// 2. Collects multi-line input via gum write (to stdout)
func GumGuidanceWriteCmd(gumPath string, ctx PhaseContext) *exec.Cmd {
	var script strings.Builder

	// Step 1: show phase context.
	mdContent := buildContextMarkdown(ctx)
	escaped := strings.ReplaceAll(mdContent, "'", "'\\''")
	script.WriteString(fmt.Sprintf("printf '%%s' '%s' | %s format --type markdown >&2\n",
		escaped, gumPath))

	// Step 2: collect multi-line guidance via gum write.
	script.WriteString(fmt.Sprintf("%s write --header 'Send guidance to agent' --placeholder 'e.g., focus on the diffview.go file first' --header.foreground '%s' --cursor.foreground '%s'\n",
		gumPath, colorAccent, colorBlueshift))

	cmd := exec.Command("sh", "-c", script.String())
	return cmd
}

// GumGuidanceInputCmd builds an *exec.Cmd that collects a single-line
// quick guidance via gum input. This is a lighter-weight alternative to
// GumGuidanceWriteCmd for one-liner instructions.
func GumGuidanceInputCmd(gumPath string, ctx PhaseContext) *exec.Cmd {
	var script strings.Builder

	// Step 1: show compact phase context.
	mdContent := buildContextMarkdown(ctx)
	escaped := strings.ReplaceAll(mdContent, "'", "'\\''")
	script.WriteString(fmt.Sprintf("printf '%%s' '%s' | %s format --type markdown >&2\n",
		escaped, gumPath))

	// Step 2: collect one-line guidance via gum input.
	script.WriteString(fmt.Sprintf("%s input --header 'Quick guidance' --placeholder 'one-line instruction' --header.foreground '%s' --cursor.foreground '%s'\n",
		gumPath, colorAccent, colorBlueshift))

	cmd := exec.Command("sh", "-c", script.String())
	return cmd
}

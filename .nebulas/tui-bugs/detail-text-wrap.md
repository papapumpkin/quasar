+++
id = "detail-text-wrap"
title = "Fix detail text overflowing past viewport border"
type = "bug"
priority = 1
depends_on = ["beads-plan-views"]
+++

## Bug

All detail panel content (agent output, plan body, beads view) renders as single long lines that spill past the viewport border. The bubbles `viewport.Model` doesn't auto-wrap; content must be pre-wrapped before `SetContent()`/`SetContentWithHeader()`.

## Fix

In `SetContent()` and `SetContentWithHeader()`, wrap the content to the viewport width before passing it to `d.viewport.SetContent()`. Use `lipgloss.NewStyle().Width(d.viewport.Width).Render(content)` which does soft word-wrapping natively with correct ANSI handling.

This fixes ALL detail views (agent output, plan body, beads view) in one place since they all flow through these two methods.

## File

`internal/tui/detailpanel.go`

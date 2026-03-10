+++
id = "hail-gum"
title = "Replace hail and dialog overlays with gum-based interactive prompts"
type = "feature"
priority = 1
depends_on = ["glamour-markdown"]
+++

## Problem

The hail system has multiple UX issues that stem from building custom BubbleTea overlays for human interaction:

1. **Hail list friction** — user must press H, select a hail, respond, press H again for the next one. No auto-advance.
2. **Dialog vs hail confusion** — two separate overlay systems (red-bordered hail, blue-bordered dialog) compete for focus in multi-phase scenarios.
3. **Compose mode is awkward** — custom textinput with Ctrl+D to send, tab to toggle scroll modes. Non-standard keybindings.
4. **ResponseCh silent drops** — non-blocking channel sends can lose responses if the receiver has exited.
5. **Auto-resolution is invisible** — hails that time out disappear silently from the pending list.
6. **Raw markdown in context** — detail text shows unrendered markdown (partially addressed by glamour-markdown phase).

Charmbracelet's `gum` CLI tool provides battle-tested interactive prompts (choose, write, confirm, filter, pager, format) that solve all of these with zero custom overlay code.

## Solution

Implement a gum-backed `dialog.Opener` and hail resolver that suspends the TUI, runs gum subprocesses for interaction, captures responses, and resumes the TUI.

### Architecture

1. **New package `internal/gum/`** — thin wrapper around gum CLI subprocess calls:
   ```go
   type Gum struct {
       BinPath string // path to gum binary, auto-detected
   }

   func (g *Gum) Choose(ctx context.Context, prompt string, options []string) (string, error)
   func (g *Gum) Write(ctx context.Context, placeholder string) (string, error)
   func (g *Gum) Confirm(ctx context.Context, prompt string) (bool, error)
   func (g *Gum) Filter(ctx context.Context, items []string) (string, error)
   func (g *Gum) Pager(ctx context.Context, content string) error
   func (g *Gum) Format(ctx context.Context, markdown string) (string, error)
   func (g *Gum) Validate() error // checks gum binary exists
   ```

   Each method builds args, runs `exec.CommandContext`, captures stdout. Stdin/stdout/stderr are wired to the terminal (not captured for interactive commands like choose/write).

2. **Gum-backed dialog opener `internal/gum/dialog.go`**:
   Implements `dialog.Opener` interface. When `Open()` is called:
   - Suspends the BubbleTea program (sends `tea.Suspend` or uses `tea.ExecProcess`)
   - Shows context via `gum format` (renders markdown)
   - Presents options via `gum choose` if options are provided
   - Falls back to `gum write` for free-text input
   - Returns the response, resumes the TUI

3. **Gum-backed hail resolver `internal/gum/hail.go`**:
   Replaces the hail list + hail detail overlays. When hails are pending:
   - If multiple hails: `gum filter` to pick which one to address first
   - Shows hail detail via `gum format` (renders markdown context)
   - If hail has options: `gum choose` with the option list
   - If free-text needed: `gum write` with placeholder
   - Returns resolution string to the hail queue
   - Auto-advances to next pending hail (no manual re-trigger)

4. **TUI integration** — the hail overlay (`hailoverlay.go`) and hail list (`hail_list.go`) become fallbacks for when gum is not available. Detection:
   - On startup, check if `gum` is in PATH
   - If available: use gum-backed resolver
   - If not: fall back to existing BubbleTea overlays (no regression)

### Interaction Flow (with gum)

```
Agent posts hail → HailQueue.Post()
  → TUI receives MsgHailReceived
  → Instead of showing overlay, suspends TUI
  → Shows:
    ┌──────────────────────────────────────────┐
    │ ⚠ HAIL: Phase "streaming-invoker"        │
    │                                          │
    │ Reviewer flagged critical finding:        │
    │ The stderr pipe is not being closed on    │
    │ context cancellation, which could leak    │
    │ goroutines.                               │
    │                                          │
    │ > retry                                  │
    │   skip                                   │
    │   fail                                   │
    │   (type custom response)                 │
    └──────────────────────────────────────────┘
  → User selects or types response
  → TUI resumes, hail resolved
  → If more pending hails: auto-advance to next
```

### Gum Styling

Use gum's built-in styling flags to match quasar's galactic palette:
- `--cursor.foreground "#7aa2f7"` (blueshift)
- `--header.foreground "#ff9e64"` (accent/amber)
- `--selected.foreground "#9ece6a"` (success/green)

### tea.Suspend / tea.ExecProcess

BubbleTea supports `tea.ExecProcess(cmd, callback)` which temporarily yields the terminal to a subprocess and resumes when it exits. This is the correct way to hand off to gum without fighting over the terminal. The existing `tea.Suspend` msg can also work but `ExecProcess` is more robust for subprocesses.

## Files

- `internal/gum/gum.go` — new package, gum CLI wrapper
- `internal/gum/gum_test.go` — tests with mock binary
- `internal/gum/dialog.go` — gum-backed dialog.Opener implementation
- `internal/gum/hail.go` — gum-backed hail resolver
- `internal/tui/model.go` — wire gum resolver when available, use tea.ExecProcess for suspension
- `internal/tui/msg.go` — add MsgGumResult or similar for async gum completion

## Acceptance Criteria

- [ ] `internal/gum/` package wraps gum CLI (choose, write, confirm, filter, format, pager)
- [ ] Gum-backed dialog opener implements `dialog.Opener` interface
- [ ] Hail resolution uses gum choose/write instead of custom overlay
- [ ] Multiple pending hails auto-advance after each resolution
- [ ] TUI suspends cleanly during gum interaction, resumes after
- [ ] Graceful fallback to existing overlays when gum binary is not in PATH
- [ ] Context/detail renders as markdown via `gum format`
- [ ] `go test ./internal/gum/...` passes
- [ ] `go build ./...` and `go vet ./...` pass

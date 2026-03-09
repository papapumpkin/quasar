+++
id = "glamour-markdown"
title = "Render markdown content with glamour"
type = "task"
priority = 1
depends_on = []
+++

## Problem

Agent output, cycle summaries, reviewer reports, and escalation dialog context contain markdown (headings, code blocks, lists, bold/italic) that renders as raw text in the TUI. `##` headers, `- [ ]` checkboxes, and triple-backtick code blocks are displayed literally, making content hard to scan.

## Solution

Add `github.com/charmbracelet/glamour` as a dependency and use it to render markdown content in the detail panel and dialog overlay.

### Approach

1. `go get github.com/charmbracelet/glamour` — this is part of the Charm ecosystem (same authors as BubbleTea/lipgloss).

2. Create a small rendering helper in `internal/tui/markdown.go`:
   ```go
   func RenderMarkdown(content string, width int) string
   ```
   - Uses `glamour.NewTermRenderer` with `glamour.DarkStyle` (matches the galactic palette)
   - Sets word wrap to `width`
   - Falls back to raw text on any render error (graceful degradation)
   - Cache the renderer (it's reusable) — create once per AppModel or as a package-level lazy init

3. Apply markdown rendering in these locations:
   - `detailpanel.go` — `FormatAgentOutput()` should render markdown instead of just `HighlightOutput()`
   - `dialog_overlay.go` — the context panel content
   - `loopview.go` — cycle summary display
   - Any other place that displays agent text in the detail panel

4. The `HighlightOutput()` function in `detailpanel.go` (APPROVED/ISSUE/SEVERITY highlighting) should be applied *after* glamour renders, or integrated into the glamour style. Evaluate which approach looks better — glamour already has syntax highlighting for code blocks which may supersede the simple keyword matching.

## Files

- `internal/tui/markdown.go` — new file, glamour rendering helper
- `internal/tui/markdown_test.go` — test basic rendering and fallback
- `internal/tui/detailpanel.go` — use RenderMarkdown in FormatAgentOutput
- `internal/tui/dialog_overlay.go` — use RenderMarkdown for context panel
- `go.mod`, `go.sum` — add glamour dependency

## Acceptance Criteria

- [ ] `go get github.com/charmbracelet/glamour` added to go.mod
- [ ] Markdown headings, code blocks, lists, bold/italic render with ANSI styling in the detail panel
- [ ] Dialog overlay context panel renders markdown
- [ ] Fallback to raw text on render error (no panics)
- [ ] Renderer is width-aware (wraps to panel width)
- [ ] `go test ./internal/tui/...` passes
- [ ] `go build ./...` and `go vet ./...` pass

+++
id = "diff-viewer"
title = "Syntax-highlighted diff viewer for per-cycle code changes"
type = "feature"
priority = 3
depends_on = ["phase-detail"]
scope = ["internal/web/handler_diff.go", "internal/web/diff_render.go", "internal/web/templates/diff.html"]
+++

## Problem

The TUI has a diff viewer (`internal/tui/diffview.go`) that parses unified diffs into `FileDiff`, `DiffHunk`, and `DiffLine` structs, then renders them with side-by-side pairs and color-coded additions/deletions. Operators need the same capability in the web dashboard to review what each coder agent changed in each cycle. Currently the `AgentDetail` stores the raw diff string, but there is no HTML rendering pipeline for it.

## Solution

Create a `/phase/{id}/diff/{cycle}` route that renders a syntax-highlighted diff view. Reuse the existing diff parsing logic from `internal/tui/diffview.go` (`ParseUnifiedDiff`, `ComputeDiffStat`, `FileDiff`, `DiffHunk`, `DiffLine`) and build an HTML renderer on top of it.

### Diff rendering

```go
// internal/web/diff_render.go

// DiffViewData is the template context for the diff viewer page.
type DiffViewData struct {
    NebulaName string
    PhaseID    string
    Cycle      int
    Files      []FileDiffView
    Stat       DiffStatView
}

// FileDiffView wraps a parsed FileDiff with HTML-ready data.
type FileDiffView struct {
    Path       string
    Hunks      []HunkView
    Additions  int
    Deletions  int
    Collapsed  bool // collapse by default if file has > 200 lines changed
}

// HunkView holds lines ready for HTML rendering.
type HunkView struct {
    Lines []DiffLineView
}

// DiffLineView is a single diff line with CSS class and line numbers.
type DiffLineView struct {
    Type    string // "context", "add", "remove"
    Content string
    OldNum  string // blank for additions
    NewNum  string // blank for deletions
}

// RenderDiffHTML parses a unified diff string and returns structured
// data ready for template rendering.
func RenderDiffHTML(raw string) DiffViewData {
    files := diffview.ParseUnifiedDiff(raw)
    stat := diffview.ComputeDiffStat(raw)
    // Map to view types...
}
```

### Syntax highlighting

Use CSS-only syntax highlighting for the diff content. The diff context already provides strong visual cues (green/red lines), so full syntax highlighting is a nice-to-have. Apply a simple approach:

1. Diff lines get CSS classes: `.diff-add`, `.diff-remove`, `.diff-context`
2. Line numbers rendered in a gutter column
3. File paths as collapsible headers

If a Go-based highlighter is warranted later, the `DiffLineView.Content` field is the injection point — but for this phase, CSS classes on the diff type are sufficient.

### Template

```html
<!-- internal/web/templates/diff.html -->
<div class="diff-viewer">
    <div class="diff-stat">
        <span>{{.Stat.FilesChanged}} file(s) changed</span>
        <span class="additions">+{{.Stat.Insertions}}</span>
        <span class="deletions">-{{.Stat.Deletions}}</span>
    </div>

    {{range .Files}}
    <details class="diff-file" {{if not .Collapsed}}open{{end}}>
        <summary class="diff-file-header">
            {{.Path}}
            <span class="file-stat">+{{.Additions}} -{{.Deletions}}</span>
        </summary>
        {{range .Hunks}}
        <table class="diff-hunk">
            {{range .Lines}}
            <tr class="diff-line diff-line--{{.Type}}">
                <td class="line-num old-num">{{.OldNum}}</td>
                <td class="line-num new-num">{{.NewNum}}</td>
                <td class="line-content"><code>{{.Content}}</code></td>
            </tr>
            {{end}}
        </table>
        {{end}}
    </details>
    {{end}}
</div>
```

### Handler

```go
// internal/web/handler_diff.go

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
    phaseID := r.PathValue("id")
    cycleStr := r.PathValue("cycle")
    cycle, err := strconv.Atoi(cycleStr)
    if err != nil {
        http.Error(w, "invalid cycle number", http.StatusBadRequest)
        return
    }

    s.mu.RLock()
    detail, ok := s.phases[phaseID]
    n := s.nebula
    s.mu.RUnlock()

    if !ok || cycle < 1 || cycle > len(detail.Cycles) {
        http.NotFound(w, r)
        return
    }

    // Find the coder agent's diff for this cycle.
    var rawDiff string
    for _, agent := range detail.Cycles[cycle-1].Agents {
        if agent.Role == "coder" && agent.Diff != "" {
            rawDiff = agent.Diff
            break
        }
    }
    if rawDiff == "" {
        http.Error(w, "no diff available for this cycle", http.StatusNotFound)
        return
    }

    data := RenderDiffHTML(rawDiff)
    data.NebulaName = n.Manifest.Nebula.Name
    data.PhaseID = phaseID
    data.Cycle = cycle

    if err := s.templates.ExecuteTemplate(w, "diff.html", data); err != nil {
        http.Error(w, "template error", http.StatusInternalServerError)
    }
}
```

### Route registration

Add to `routes()`:

```go
s.mux.HandleFunc("GET /phase/{id}/diff/{cycle}", s.handleDiff)
```

### AgentDetail update

Extend `AgentDetail` (from phase 4) to also store the raw diff:

```go
type AgentDetail struct {
    // ... existing fields ...
    Diff     string // raw unified diff output
    DiffFiles []string // list of changed file paths
}
```

## Files

- `internal/web/handler_diff.go` — `handleDiff` handler
- `internal/web/handler_diff_test.go` — test with sample unified diffs, missing diff, invalid cycle
- `internal/web/diff_render.go` — `DiffViewData`, `FileDiffView`, `HunkView`, `DiffLineView`, `RenderDiffHTML`
- `internal/web/diff_render_test.go` — test parsing and rendering of multi-file diffs with additions, deletions, and context
- `internal/web/templates/diff.html` — diff viewer page template
- `internal/web/static/style.css` — add diff-specific styles (line colors, gutter, collapsible file sections)
- `internal/web/phase_state.go` — extend `AgentDetail` with `Diff` and `DiffFiles` fields
- `internal/web/server.go` — register `/phase/{id}/diff/{cycle}` route

## Acceptance Criteria

- [ ] `GET /phase/{id}/diff/{cycle}` returns an HTML page with file-by-file diff rendering
- [ ] Each file section shows path, addition/deletion counts, and is collapsible via `<details>`
- [ ] Diff lines have correct CSS classes: `.diff-line--add`, `.diff-line--remove`, `.diff-line--context`
- [ ] Line numbers render in gutter columns (old and new)
- [ ] Diff stat summary shows total files changed, insertions, and deletions
- [ ] Files with >200 changed lines default to collapsed state
- [ ] Invalid cycle numbers return 400; missing diffs return 404
- [ ] Links from the phase detail page to `/phase/{id}/diff/{cycle}` work correctly
- [ ] `go test ./internal/web/...` passes

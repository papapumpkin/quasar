+++
id = "process-drilldown"
title = "Below-inspired hierarchical drill-down with collapsible tree navigation"
type = "feature"
priority = 2
depends_on = ["split-pane-layout"]
+++

## Problem

The current drill-down is linear: phases → phase loop → agent output, navigated via Enter/Esc with a breadcrumb trail. There's no way to see the hierarchy at a glance — you're either looking at the phase list OR one phase's details, never both. Below's cgroup view lets you expand/collapse nodes inline and see the full tree with metrics at every level.

## Solution

Replace the linear drill-down with an expandable tree view inspired by Below's cgroup hierarchy. Phases become expandable nodes; their cycles and agents become child nodes. The tree lives in the sidebar (from phase 08), and expanding a node updates the right panel with contextual detail.

### Tree Structure

```
nebula: my-nebula (3/8 · $2.41)
├─ ✓ glamour-markdown                    $0.82  2 cycles
│  ├─ cycle 1                            $0.65
│  │  ├─ coder   $0.42  18.2s
│  │  └─ reviewer $0.23  12.1s  ⚠ 1 issue
│  └─ cycle 2                            $0.17
│     ├─ coder   $0.12   8.4s
│     └─ reviewer $0.05   4.2s  ✓ approved
├─ ● unified-diff                        $0.91  cycle 2/5
│  ├─ cycle 1                            $0.54
│  │  ├─ coder   $0.38  22.1s
│  │  └─ reviewer $0.16   9.8s  ⚠ 3 issues
│  └─ cycle 2 (active)
│     └─ coder   $0.37  coding...
├─ ○ board-phase-names                   queued
├─ ○ line-count-bug                      queued
│  └─ (blocked by: unified-diff)
└─ ...
```

### Approach

1. **Tree node types**: Each level in the hierarchy is a `TreeNode`:
   ```go
   type TreeNode struct {
       Kind     TreeNodeKind  // nebula, phase, cycle, agent
       ID       string
       Label    string
       Status   string        // icon + status text
       Metrics  string        // cost, duration, issues — right-aligned
       Children []*TreeNode
       Expanded bool
       Depth    int
   }
   ```

2. **Collapse/expand**: Enter toggles expand/collapse on the selected node. When a phase is collapsed, only the summary line shows. When expanded, cycles and agents appear as children. Below-style: `=` collapses children of current node.

3. **Keyboard shortcuts from Below**:
   - `C` — sort phases by cost
   - `D` — sort by duration
   - `=` — collapse selected node's children
   - Enter — toggle expand/collapse
   - `z` — zoom: expand selected node, collapse all others

4. **Metrics columns**: Like Below's tab-based column sets, support multiple metric views:
   - **Default**: status, cost, cycles
   - **Performance** (press `P`): duration per agent, tokens used
   - **Issues** (press `I`): issue count, satisfaction, risk level
   - Columns render right-aligned after the tree indentation

5. **Auto-expand active**: Phases currently being worked on auto-expand to show the active cycle and agent. New cycle starts auto-expand the parent phase.

6. **Detail panel sync**: Selecting any tree node updates the right detail panel:
   - Nebula root → overall progress summary
   - Phase → phase description + latest reviewer report
   - Cycle → cycle summary (coder output + reviewer feedback)
   - Agent → full agent output text (with glamour markdown rendering)

7. **Integration with sidebar**: This replaces the flat phase list from `split-pane-layout` (phase 08). The sidebar becomes a tree view. The right panels stay the same.

## Files

- `internal/tui/treeview.go` — new file, tree node types and rendering
- `internal/tui/treeview_test.go` — tests for tree rendering and collapse/expand
- `internal/tui/sidebar.go` — update to use tree view instead of flat list (from phase 08)
- `internal/tui/model.go` — wire tree navigation, sync detail panel on selection

## Acceptance Criteria

- [ ] Phases render as expandable tree nodes in the sidebar
- [ ] Cycles and agents appear as children when a phase is expanded
- [ ] Enter toggles expand/collapse
- [ ] Active phases auto-expand to show current cycle
- [ ] Metrics (cost, duration, status) render right-aligned at each level
- [ ] Selecting a node updates the detail panel with contextual content
- [ ] Sort shortcuts (C for cost, D for duration) reorder phases
- [ ] `=` collapses children of selected node
- [ ] `go test ./internal/tui/...` passes
- [ ] `go build ./...` and `go vet ./...` pass

+++
id = "model-integration"
title = "Wire cockpit views into AppModel"
type = "task"
priority = 2
depends_on = ["board-view", "worker-cards", "tab-system", "stats-bar-upgrade", "decision-overlay", "entanglement-viewer", "scratchpad-view"]
scope = ["internal/tui/model.go", "internal/tui/keys.go", "internal/tui/footer.go"]
+++

## Problem

All the cockpit components (board view, quasar cards, tabs, stats bar, hail overlay, entanglement viewer, scratchpad) exist as standalone components. They need to be wired into `AppModel` so they participate in the Bubble Tea update/render cycle, receive messages, and coordinate with the existing views.

## Solution

Integrate all cockpit components into `AppModel`:

### Model fields

Add to `AppModel`:
```go
Board           BoardView
QuasarCards     map[string]*WorkerCard
TabBar          TabBar
ActiveTab       CockpitTab
Entanglements   EntanglementView
Scratchpad      ScratchpadView
Hail            *HailOverlay
BoardActive     bool // true = board view, false = table view
```

### View toggle

Add a keybinding (`b` key) to toggle between `BoardActive = true` (columnar board) and `BoardActive = false` (existing `NebulaView` table). Default to the board view for terminals >= `BoardMinWidth` (100 cols), table view otherwise.

### Message routing

In `AppModel.Update()`, route messages to the new components:

- `MsgPhaseTaskStarted` / `MsgPhaseTaskComplete` → update `Board.phases` state, create/remove `QuasarCards` entries
- `MsgPhaseCycleStart` → update `QuasarCards[phaseID].Cycle`
- `MsgPhaseAgentStart` → update `QuasarCards[phaseID].AgentRole`, `Activity`
- `MsgPhaseAgentDone` → update `QuasarCards[phaseID].TokensUsed`, `StatusBar.TotalTokens`
- `MsgHail` → if `BoardActive`, create `HailOverlay`; otherwise fall through to existing `GatePrompt`
- `MsgEntanglementUpdate` → update `Entanglements.entanglements`
- `MsgScratchpadEntry` → append to `Scratchpad.entries`
- `MsgDiscoveryPosted` → show toast notification with discovery kind
- `MsgStaleWarning` → show toast with stale state warning

### Render composition

In `AppModel.View()`, when in nebula mode and `BoardActive`:

```
┌─ StatusBar ──────────────────────────────────────────┐
│ TabBar                                                │
├──────────────────────────────────────────────────────┤
│                                                       │
│  <Active Tab Content>                                 │
│    TabBoard: BoardView + QuasarCards                  │
│    TabEntanglements: EntanglementView                 │
│    TabScratchpad: ScratchpadView                      │
│                                                       │
│  (HailOverlay floats on top if present)               │
│                                                       │
├──────────────────────────────────────────────────────┤
│ BottomBar (stats)                                     │
│ Footer (keybinds)                                     │
└──────────────────────────────────────────────────────┘
```

Use `lipgloss.JoinVertical` to compose the layers. The `HailOverlay` uses `lipgloss.Place` to center over the content area.

### Keybindings update

Add to `keys.go`:
- `b` — toggle board/table view
- `Tab` / `Shift+Tab` — cycle tabs (delegated to `TabBar`)
- `1`, `2`, `3` — direct tab jump

Update `footer.go` to show cockpit-specific keybinds when `BoardActive` is true:
`[tab] entanglements  [d] detail  [r] retry  [enter] respond  [q] quit`

### Window sizing

Pass terminal dimensions to all new components on `tea.WindowSizeMsg`. The `BoardView` and `QuasarCards` need width/height for adaptive layout. If terminal width drops below `BoardMinWidth`, auto-switch to table view.

## Files

- `internal/tui/model.go` — Add cockpit fields, wire Update/View
- `internal/tui/keys.go` — Add board toggle and tab keybindings
- `internal/tui/footer.go` — Cockpit-specific keybind hints

## Acceptance Criteria

- [ ] `b` key toggles between board and table views
- [ ] Tab navigation switches content between Board/Entanglements/Scratchpad
- [ ] All existing `MsgPhase*` messages flow correctly to board and quasar cards
- [ ] `MsgHail` triggers `HailOverlay` when board view is active
- [ ] `MsgEntanglementUpdate` feeds the entanglement viewer
- [ ] `MsgScratchpadEntry` feeds the scratchpad
- [ ] `MsgDiscoveryPosted` and `MsgStaleWarning` show toast notifications
- [ ] Bottom stats bar renders below content
- [ ] Footer shows cockpit keybinds when board is active
- [ ] Auto-fallback to table view on narrow terminals
- [ ] Existing table/detail/drill-down navigation still works when board is not active
- [ ] All existing tests continue to pass
- [ ] `go test ./internal/tui/...` passes
- [ ] `go vet ./...` clean

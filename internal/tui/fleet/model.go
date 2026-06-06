package fleet

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// inFlightTick is the background refresh interval for the in-flight lane. Only
// that lane auto-refreshes; the others refresh on explicit user action (R).
const inFlightTick = 2 * time.Second

// viewMode is the model's current interaction mode.
type viewMode int

const (
	modeFleet viewMode = iota
	modeFilter
	modeReject
	modeDetail       // in-flight run detail (trace + pause/resume/kill)
	modeNebulaDetail // read-only draft/recent nebula inspection
)

// Model is the top-level fleet dashboard tea.Model. It owns the loaded fleet,
// cursor, filter, persisted UI state, and the active sub-view.
type Model struct {
	ctx       context.Context
	store     *Store
	statePath string

	full Fleet // unfiltered, as loaded
	view Fleet // full with fold + filter applied
	ui   UIState
	filt Filter

	lane   int // 0=awaiting, 1=in-flight, 2=recent
	cursor int // index within the active lane's flattened cards

	mode     viewMode
	input    string // filter / reject text entry buffer
	gitStrip bool

	detail    RunCard
	trace     []Invocation
	nebDetail NebulaDetail

	// gitSummaries caches per-repo git-status strings (keyed by repo path),
	// populated off the render path by gitCmd. View() must never shell out.
	gitSummaries map[string]string

	width, height int
	status        string // transient status line
	err           error
	quitting      bool
}

// NewModel constructs a fleet Model. statePath is the tui-state.json location.
func NewModel(ctx context.Context, store *Store, statePath string) Model {
	ui, _ := LoadState(statePath) // a bad/missing state file must not block launch
	m := Model{ctx: ctx, store: store, statePath: statePath, ui: ui}
	m.filt = ParseFilter(ui.Filter)
	switch ui.ActiveLane {
	case "in_flight":
		m.lane = 1
	case "recent":
		m.lane = 2
	}
	return m
}

// --- messages ---

type fleetLoadedMsg struct{ fleet Fleet }
type inflightLoadedMsg struct{ fleet Fleet }
type traceLoadedMsg struct{ trace []Invocation }
type nebulaDetailLoadedMsg struct{ detail NebulaDetail }
type gitLoadedMsg struct{ summaries map[string]string }
type errMsg struct{ err error }
type tickMsg struct{}

// Init loads the fleet and starts the in-flight refresh tick.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadCmd(), tickCmd())
}

// loadCmd re-queries the full fleet from SQLite.
func (m Model) loadCmd() tea.Cmd {
	return func() tea.Msg {
		f, err := m.store.Load(m.ctx)
		if err != nil {
			return errMsg{err}
		}
		return fleetLoadedMsg{f}
	}
}

// inflightCmd re-queries only the in-flight lane (the sole auto-refreshed lane).
func (m Model) inflightCmd() tea.Cmd {
	return func() tea.Msg {
		f, err := m.store.LoadInFlight(m.ctx)
		if err != nil {
			return errMsg{err}
		}
		return inflightLoadedMsg{f}
	}
}

// traceCmd loads a run's star-invocation trace.
func (m Model) traceCmd(runID string) tea.Cmd {
	return func() tea.Msg {
		t, err := m.store.Trace(m.ctx, runID)
		if err != nil {
			return errMsg{err}
		}
		return traceLoadedMsg{t}
	}
}

// nebulaDetailCmd loads a nebula's read-only inspection projection.
func (m Model) nebulaDetailCmd(id string) tea.Cmd {
	return func() tea.Msg {
		d, err := m.store.NebulaDetail(m.ctx, id)
		if err != nil {
			return errMsg{err}
		}
		return nebulaDetailLoadedMsg{d}
	}
}

// gitCmd probes git status for every repo OFF the render path and returns the
// summaries for caching. View() reads only the cached map, so the subprocess
// I/O never blocks the UI goroutine. It uses m.ctx so it cancels on quit.
func (m Model) gitCmd() tea.Cmd {
	repos := m.full.Repos
	ctx := m.ctx
	return func() tea.Msg {
		out := make(map[string]string, len(repos))
		for _, r := range repos {
			out[r.Path] = gitSummaryContext(ctx, r.Path)
		}
		return gitLoadedMsg{out}
	}
}

// tickCmd schedules the next in-flight refresh tick.
func tickCmd() tea.Cmd {
	return tea.Tick(inFlightTick, func(time.Time) tea.Msg { return tickMsg{} })
}

// Update routes messages to the active mode's handler.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case fleetLoadedMsg:
		m.full = msg.fleet
		m.recompute()
		return m, nil
	case inflightLoadedMsg:
		m.mergeInFlight(msg.fleet)
		m.recompute()
		return m, nil
	case traceLoadedMsg:
		m.trace = msg.trace
		return m, nil
	case nebulaDetailLoadedMsg:
		m.nebDetail = msg.detail
		return m, nil
	case gitLoadedMsg:
		m.gitSummaries = msg.summaries
		return m, nil
	case errMsg:
		m.err = msg.err
		return m, nil
	case tickMsg:
		if m.mode == modeDetail {
			// Skip the trace poll for the transient post-approve placeholder
			// (empty RunID): the architect run does not exist until the Phase-5
			// supervisor creates it, so there is nothing to query yet.
			if m.detail.RunID == "" {
				return m, tickCmd()
			}
			return m, tea.Batch(m.traceCmd(m.detail.RunID), tickCmd())
		}
		return m, tea.Batch(m.inflightCmd(), tickCmd())
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey dispatches a keypress based on the active mode.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeFilter:
		return m.handleTextEntry(msg, m.applyFilter)
	case modeReject:
		return m.handleTextEntry(msg, m.applyReject)
	case modeDetail:
		return m.handleDetailKey(msg)
	case modeNebulaDetail:
		return m.handleNebulaDetailKey(msg)
	default:
		return m.handleFleetKey(msg)
	}
}

// handleFleetKey handles keys in the main three-lane view.
func (m Model) handleFleetKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		m.persist()
		return m, tea.Quit
	case "tab":
		m.lane = (m.lane + 1) % 3
		m.cursor = 0
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < m.laneLen()-1 {
			m.cursor++
		}
	case "R":
		m.status = "refreshing…"
		if m.gitStrip {
			// Refresh the git strip alongside the full re-query.
			return m, tea.Batch(m.loadCmd(), m.gitCmd())
		}
		return m, m.loadCmd()
	case "g":
		m.gitStrip = !m.gitStrip
		if m.gitStrip {
			// Probe git off the render path when the strip is turned on.
			return m, m.gitCmd()
		}
	case "/":
		m.mode = modeFilter
		m.input = m.ui.Filter
	case "f":
		m.toggleFold()
	case "a":
		return m.approve(false)
	case "A":
		return m.approve(true)
	case "r":
		if m.lane == 0 && m.selectedNebula() != nil {
			m.mode = modeReject
			m.input = ""
		}
	case "d":
		if m.lane == 1 {
			return m.openDetail()
		}
		return m.openNebulaDetail()
	}
	return m, nil
}

// handleDetailKey handles keys in the run-detail view.
func (m Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "b", "esc":
		m.mode = modeFleet
		return m, nil
	case "q", "ctrl+c":
		m.quitting = true
		m.persist()
		return m, tea.Quit
	case "p":
		return m.setRunState("paused")
	case "P":
		return m.setRunState("running")
	case "k":
		return m.setRunState("killed")
	}
	return m, nil
}

// handleTextEntry drives a single-line text prompt (filter or reject reason).
func (m Model) handleTextEntry(msg tea.KeyMsg, commit func(Model) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeFleet
		m.input = ""
		return m, nil
	case "enter":
		return commit(m)
	case "backspace":
		if r := []rune(m.input); len(r) > 0 {
			m.input = string(r[:len(r)-1])
		}
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.input += string(msg.Runes)
		}
		return m, nil
	}
}

// applyFilter commits the filter prompt.
func (m Model) applyFilter(mm Model) (tea.Model, tea.Cmd) {
	mm.ui.Filter = mm.input
	mm.filt = ParseFilter(mm.input)
	mm.mode = modeFleet
	mm.cursor = 0
	mm.recompute()
	return mm, nil
}

// applyReject commits the reject prompt for the selected nebula.
func (m Model) applyReject(mm Model) (tea.Model, tea.Cmd) {
	card := mm.selectedNebula()
	mm.mode = modeFleet
	if card == nil {
		return mm, nil
	}
	if err := mm.store.Reject(mm.ctx, card.ID, mm.input); err != nil {
		mm.err = err
		return mm, nil
	}
	mm.input = ""
	mm.status = "rejected"
	return mm, mm.loadCmd()
}

// approve approves the selected awaiting-approval nebula, optionally jumping to
// the run detail view.
func (m Model) approve(jump bool) (tea.Model, tea.Cmd) {
	if m.lane != 0 {
		return m, nil
	}
	card := m.selectedNebula()
	if card == nil {
		return m, nil
	}
	if err := m.store.Approve(m.ctx, card.ID); err != nil {
		m.err = err
		return m, nil
	}
	m.status = "approved " + card.Title
	if jump {
		// Transient placeholder: the architect run does not exist yet — the
		// Phase-5 supervisor creates it asynchronously from the trigger_queue
		// row. RunID is empty until then, so the detail view shows "starting"
		// and skips the trace poll (see the tickMsg handler). Resolving the
		// view to the real run by NebulaID lands with the runtime in Phase 5.
		m.mode = modeDetail
		m.detail = RunCard{NebulaID: card.ID, NebulaTitle: card.Title, ConstellationName: "architect", State: "starting"}
		m.trace = nil
	}
	return m, m.loadCmd()
}

// handleNebulaDetailKey handles keys in the read-only nebula-detail view.
func (m Model) handleNebulaDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "b", "esc":
		m.mode = modeFleet
		return m, nil
	case "q", "ctrl+c":
		m.quitting = true
		m.persist()
		return m, tea.Quit
	}
	return m, nil
}

// openNebulaDetail opens the read-only detail view for the selected awaiting or
// recent nebula card so the operator can read its seed prompt and provenance
// before approving/rejecting. It seeds the view from the card so it is not blank
// while the full row loads.
func (m Model) openNebulaDetail() (tea.Model, tea.Cmd) {
	card := m.selectedNebula()
	if card == nil {
		return m, nil
	}
	m.mode = modeNebulaDetail
	m.nebDetail = NebulaDetail{ID: card.ID, Title: card.Title, SourceLabel: card.SourceLabel, Status: card.Status}
	return m, m.nebulaDetailCmd(card.ID)
}

// openDetail opens the detail view for the selected in-flight run.
func (m Model) openDetail() (tea.Model, tea.Cmd) {
	run := m.selectedRun()
	if run == nil {
		return m, nil
	}
	m.mode = modeDetail
	m.detail = *run
	m.trace = nil
	return m, m.traceCmd(run.RunID)
}

// setRunState applies a pause/resume/kill to the detail view's run.
func (m Model) setRunState(state string) (tea.Model, tea.Cmd) {
	if m.detail.RunID == "" {
		return m, nil
	}
	if err := m.store.SetRunState(m.ctx, m.detail.RunID, state); err != nil {
		m.err = err
		return m, nil
	}
	m.detail.State = state
	return m, m.loadCmd()
}

// toggleFold folds/unfolds the repo under the cursor. It uses a pointer
// receiver so the mutated ui/view/cursor propagate back to the caller's model
// (handleFleetKey holds an addressable local, so m.toggleFold() auto-takes &m).
// A folded repo keeps one selectable header slot, so unfold is always reachable.
func (m *Model) toggleFold() {
	if len(m.view.Repos) == 0 {
		return
	}
	repo := m.repoForCursor()
	if repo == "" {
		repo = m.view.Repos[0].DisplayName
	}
	m.ui = m.ui.ToggleFold(repo)
	m.recompute()
}

// recompute rebuilds the displayed fleet from the full fleet, applying fold
// state and the active filter, then clamps the cursor.
func (m *Model) recompute() {
	folded := applyFold(m.full, m.ui)
	m.view = m.filt.Apply(folded)
	if m.cursor >= m.laneLen() {
		m.cursor = max(0, m.laneLen()-1)
	}
}

// mergeInFlight replaces only the in-flight lane data from a fresh load.
func (m *Model) mergeInFlight(fresh Fleet) {
	byPath := make(map[string][]RunCard, len(fresh.Repos))
	for _, r := range fresh.Repos {
		byPath[r.Path] = r.InFlight
	}
	for i := range m.full.Repos {
		m.full.Repos[i].InFlight = byPath[m.full.Repos[i].Path]
	}
}

// persist saves the current UI state to disk (best effort, on quit). A write
// failure is non-fatal — the view is recoverable from defaults next launch.
func (m Model) persist() {
	st := m.ui
	st.ActiveLane = []string{"awaiting_approval", "in_flight", "recent"}[m.lane]
	if err := SaveState(m.statePath, st); err != nil {
		fmt.Fprintf(os.Stderr, "fleet: save ui state: %v\n", err)
	}
}

// applyFold returns a copy of the fleet with Folded set per the UI state.
func applyFold(in Fleet, ui UIState) Fleet {
	out := Fleet{Repos: make([]RepoLane, len(in.Repos))}
	copy(out.Repos, in.Repos)
	for i := range out.Repos {
		out.Repos[i].Folded = ui.IsFolded(out.Repos[i].DisplayName)
	}
	return out
}

// --- selection helpers ---

// repoSlots returns how many selectable cursor positions a repo contributes in
// the active lane: a folded repo keeps one slot (its header, so unfold stays
// reachable), an unfolded repo contributes one slot per card. All cursor
// accounting (laneLen, selectedNebula, selectedRun, repoForCursor) shares this
// so selection and clamping never diverge across folded repos.
func (m Model) repoSlots(r RepoLane) int {
	if r.Folded {
		return 1
	}
	switch m.lane {
	case 0:
		return len(r.AwaitingApproval)
	case 1:
		return len(r.InFlight)
	default:
		return len(r.Recent)
	}
}

// laneLen returns the number of selectable cursor positions in the active lane.
func (m Model) laneLen() int {
	n := 0
	for _, r := range m.view.Repos {
		n += m.repoSlots(r)
	}
	return n
}

// selectedNebula returns the nebula card under the cursor (awaiting/recent
// lanes). It returns nil on the in-flight lane and when the cursor rests on a
// folded repo's header slot (which carries no card).
func (m Model) selectedNebula() *NebulaCard {
	if m.lane == 1 {
		return nil
	}
	idx := 0
	for ri := range m.view.Repos {
		r := m.view.Repos[ri]
		if r.Folded {
			idx++ // header slot — not a card
			continue
		}
		cards := r.AwaitingApproval
		if m.lane == 2 {
			cards = r.Recent
		}
		for ci := range cards {
			if idx == m.cursor {
				return &cards[ci]
			}
			idx++
		}
	}
	return nil
}

// selectedRun returns the run card under the cursor (in-flight lane). It returns
// nil when the cursor rests on a folded repo's header slot.
func (m Model) selectedRun() *RunCard {
	if m.lane != 1 {
		return nil
	}
	idx := 0
	for ri := range m.view.Repos {
		r := m.view.Repos[ri]
		if r.Folded {
			idx++ // header slot — not a run
			continue
		}
		for ci := range r.InFlight {
			if idx == m.cursor {
				return &m.view.Repos[ri].InFlight[ci]
			}
			idx++
		}
	}
	return nil
}

// repoForCursor returns the display name of the repo owning the cursor's slot,
// whether that slot is a card or a folded repo's header.
func (m Model) repoForCursor() string {
	idx := 0
	for _, r := range m.view.Repos {
		n := m.repoSlots(r)
		if m.cursor < idx+n {
			return r.DisplayName
		}
		idx += n
	}
	return ""
}

// selection resolves the cursor to a render Selection so the active lane's
// selected card (or a folded repo's header) is visibly marked. It shares the
// repoSlots accounting with the cursor-navigation helpers, so the highlight
// always matches what approve/reject/detail will act on. Active is false when
// the lane has no selectable slots.
func (m Model) selection() Selection {
	idx := 0
	for ri, r := range m.view.Repos {
		n := m.repoSlots(r)
		if n == 0 {
			continue
		}
		if m.cursor < idx+n {
			cardIdx := headerCard // folded repo → its header is the slot
			if !r.Folded {
				cardIdx = m.cursor - idx
			}
			return Selection{Lane: m.lane, RepoIdx: ri, CardIdx: cardIdx, Active: true}
		}
		idx += n
	}
	return Selection{Lane: m.lane}
}

// View renders the active mode.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	switch m.mode {
	case modeDetail:
		return m.detailView()
	case modeNebulaDetail:
		return m.nebulaDetailView()
	default:
		return m.fleetView()
	}
}

// fleetView renders the three-lane view plus footer and any prompt/strip.
func (m Model) fleetView() string {
	width := m.width
	if width <= 0 {
		width = 110
	}
	var b strings.Builder
	b.WriteString(RenderFleet(m.view, width, m.selection()))
	b.WriteString("\n\n")
	if m.gitStrip {
		b.WriteString(m.gitStripView())
		b.WriteByte('\n')
	}
	switch m.mode {
	case modeFilter:
		b.WriteString("filter: " + m.input + "▏\n")
	case modeReject:
		b.WriteString("reject reason: " + m.input + "▏\n")
	default:
		b.WriteString(m.footer())
		b.WriteByte('\n')
	}
	if m.err != nil {
		b.WriteString("error: " + m.err.Error() + "\n")
	} else if m.status != "" {
		b.WriteString(m.status + "\n")
	}
	return b.String()
}

// footer renders the keybinding hint line, marking the active lane. Lane-
// specific actions are shown only where they apply so no hint advertises a
// dead keybinding: approve/reject on the awaiting lane, details on in-flight.
func (m Model) footer() string {
	var actions string
	switch m.lane {
	case 0:
		actions = "[a] approve  [r] reject  "
	case 1:
		actions = "[d] details  "
	}
	return fmt.Sprintf("[lane %d/3] %s[tab] switch  [/] filter  [f] fold  [g] git  [R] refresh  [q] quit",
		m.lane+1, actions)
}

// gitStripView renders the informational per-repo git-status strip from the
// cached summaries only — it never shells out. Repos without a cached entry yet
// show "loading…" until gitCmd's result arrives.
func (m Model) gitStripView() string {
	var b strings.Builder
	b.WriteString("── git ──\n")
	for _, r := range m.full.Repos {
		summary, ok := m.gitSummaries[r.Path]
		if !ok {
			summary = "loading…"
		}
		fmt.Fprintf(&b, "  %s: %s\n", r.DisplayName, summary)
	}
	return strings.TrimRight(b.String(), "\n")
}

// detailView renders the run detail view: header, controls, and step trace.
// When RunID is empty the run has just been approved but not yet created by the
// supervisor, so it renders an explicit "starting" placeholder instead of a
// half-empty run header.
func (m Model) detailView() string {
	var b strings.Builder
	if m.detail.RunID == "" {
		fmt.Fprintf(&b, "%s — %s\n", m.detail.NebulaTitle, m.detail.ConstellationName)
		b.WriteString("architect starting — run not yet created\n\n")
		b.WriteString("[b] back  [q] quit\n")
		return b.String()
	}
	fmt.Fprintf(&b, "Run %s — %s (%s)\n", shortRunID(m.detail.RunID), m.detail.NebulaTitle, m.detail.State)
	fmt.Fprintf(&b, "constellation: %s   node: %s   step %d/%d\n\n",
		m.detail.ConstellationName, m.detail.CurrentNode, m.detail.StepIndex, m.detail.StepCount)
	b.WriteString(RenderTrace(m.trace))
	b.WriteString("\n\n[p] pause  [P] resume  [k] kill  [b] back  [q] quit\n")
	if m.err != nil {
		b.WriteString("error: " + m.err.Error() + "\n")
	}
	return b.String()
}

// nebulaDetailView renders the read-only inspection projection of a nebula card
// (seed prompt and provenance) shown when the operator opens a draft or recent
// nebula with `d`.
func (m Model) nebulaDetailView() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n", m.nebDetail.Title, m.nebDetail.Status)
	source := m.nebDetail.SourceLabel
	if m.nebDetail.SourceURL != "" {
		source += " — " + m.nebDetail.SourceURL
	}
	fmt.Fprintf(&b, "source: %s\n\n", source)
	if m.nebDetail.Description != "" {
		b.WriteString(m.nebDetail.Description)
		b.WriteString("\n\n")
	}
	b.WriteString("[b] back  [q] quit\n")
	if m.err != nil {
		b.WriteString("error: " + m.err.Error() + "\n")
	}
	return b.String()
}

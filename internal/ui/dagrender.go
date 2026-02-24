// Package ui provides stderr-based UI output for Quasar.
// This file implements a box-and-arrow ASCII/ANSI DAG renderer.
package ui

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/papapumpkin/quasar/internal/ansi"
	"github.com/papapumpkin/quasar/internal/dag"
)

// NodeStatus holds the live state of a single DAG node for rendering.
type NodeStatus struct {
	State  string  // "queued", "running", "done", "failed", "blocked"
	Cost   float64 // accumulated USD cost
	Cycles int     // review cycles used
}

// DAGRenderer produces an ASCII visualization of a directed acyclic graph.
// Nodes are drawn as boxes with status-colored borders. Edges are drawn
// with Unicode line-drawing characters. The renderer operates in two modes:
// full-box mode (<=10 nodes) and compact single-line mode (>10 nodes).
type DAGRenderer struct {
	// Width is the available terminal width in columns.
	Width int

	// UseColor controls whether ANSI escape codes are emitted.
	UseColor bool

	// StatusFunc returns the current status of a node by ID.
	// If nil, all nodes are rendered in "queued" style.
	StatusFunc func(id string) NodeStatus

	// CriticalPath is the set of node IDs on the critical path.
	// These are highlighted with bold styling.
	CriticalPath map[string]bool

	// TrackMap maps node ID to track ID for visual grouping.
	TrackMap map[string]int
}

// compactThreshold is the number of total nodes above which the renderer
// switches from full-box mode to compact single-line mode.
const compactThreshold = 10

// Render produces the ASCII DAG string. Waves are rows; within each wave,
// nodes are rendered side by side. Dependencies between waves are connected
// with Unicode box-drawing characters.
func (r *DAGRenderer) Render(waves []dag.Wave, deps map[string][]string, titles map[string]string) string {
	if len(waves) == 0 {
		return ""
	}

	totalNodes := 0
	for _, w := range waves {
		totalNodes += len(w.NodeIDs)
	}
	if totalNodes == 0 {
		return ""
	}

	width := r.Width
	if width <= 0 {
		width = 80
	}

	if totalNodes > compactThreshold {
		return r.renderCompact(waves, deps, titles, width)
	}
	return r.renderFull(waves, deps, titles, width)
}

// ────────────────────────── full-box mode ──────────────────────────

// nodeBox represents the rendered text and position of a single node box.
type nodeBox struct {
	id     string
	lines  []string // rendered lines (including border)
	width  int      // max line width in runes
	center int      // horizontal center column in the output
}

func (r *DAGRenderer) renderFull(waves []dag.Wave, deps map[string][]string, titles map[string]string, width int) string {
	// Phase 1: build box text for every node.
	boxes := make(map[string]*nodeBox, len(titles))
	for _, w := range waves {
		for _, id := range w.NodeIDs {
			boxes[id] = r.buildBox(id, titles[id])
		}
	}

	// Phase 2: lay out each wave row and assign centers.
	var sb strings.Builder
	for wi, w := range waves {
		row := make([]*nodeBox, len(w.NodeIDs))
		for i, id := range w.NodeIDs {
			row[i] = boxes[id]
		}
		r.layoutRow(row, width)

		// Draw connectors from previous wave to this wave.
		if wi > 0 {
			r.drawConnectors(&sb, waves[wi-1], w, boxes, deps, width)
		}

		// Draw the node boxes.
		r.drawRow(&sb, row, width)
	}

	return sb.String()
}

// buildBox creates a full-box representation for a node.
//
//	┌──────────────────┐
//	│ spacetime-model   │
//	│ $2.34  3/5 cyc   │   (only if StatusFunc is non-nil and has data)
//	└──────────────────┘
func (r *DAGRenderer) buildBox(id, title string) *nodeBox {
	if title == "" {
		title = id
	}

	status := NodeStatus{State: "queued"}
	if r.StatusFunc != nil {
		status = r.StatusFunc(id)
	}

	// Build content lines.
	var contentLines []string
	contentLines = append(contentLines, title)

	if status.Cost > 0 || status.Cycles > 0 {
		detail := fmt.Sprintf("$%.2f  %d cyc", status.Cost, status.Cycles)
		contentLines = append(contentLines, detail)
	}

	// Determine inner width (widest content line in runes, not bytes).
	inner := 0
	for _, line := range contentLines {
		if w := utf8.RuneCountInString(line); w > inner {
			inner = w
		}
	}
	// Minimum inner width of 6 to avoid tiny boxes.
	if inner < 6 {
		inner = 6
	}

	borderStyle := r.borderStyle(id)
	topLeft, topRight, bottomLeft, bottomRight, horiz, vert := borderStyle[0], borderStyle[1], borderStyle[2], borderStyle[3], borderStyle[4], borderStyle[5]

	// Build rendered lines.
	var lines []string
	topBorder := string(topLeft) + strings.Repeat(string(horiz), inner+2) + string(topRight)
	lines = append(lines, r.colorize(topBorder, id, status.State))

	for _, cl := range contentLines {
		padded := cl + strings.Repeat(" ", inner-utf8.RuneCountInString(cl))
		line := string(vert) + " " + padded + " " + string(vert)
		lines = append(lines, r.colorize(line, id, status.State))
	}

	bottomBorder := string(bottomLeft) + strings.Repeat(string(horiz), inner+2) + string(bottomRight)
	lines = append(lines, r.colorize(bottomBorder, id, status.State))

	return &nodeBox{
		id:    id,
		lines: lines,
		width: inner + 4, // borders + padding
	}
}

// borderStyle returns the 6 box-drawing runes [TL, TR, BL, BR, H, V] based
// on the node's track.
func (r *DAGRenderer) borderStyle(id string) [6]rune {
	if r.TrackMap != nil {
		if track, ok := r.TrackMap[id]; ok && track > 0 {
			// Track 1+: double-line border.
			return [6]rune{'╔', '╗', '╚', '╝', '═', '║'}
		}
	}
	// Track 0 / unknown: single-line border.
	return [6]rune{'┌', '┐', '└', '┘', '─', '│'}
}

// colorize wraps text in ANSI color codes based on node status and whether
// the node is on the critical path.
func (r *DAGRenderer) colorize(text, id, state string) string {
	if !r.UseColor {
		return text
	}

	var prefix string
	switch state {
	case "done":
		prefix = ansi.Green
	case "running":
		prefix = ansi.Yellow
	case "failed":
		prefix = ansi.Red
	case "blocked":
		prefix = ansi.Magenta
	default: // queued
		prefix = ansi.Blue
	}

	if r.CriticalPath[id] {
		prefix = ansi.Bold + prefix
	}

	return prefix + text + ansi.Reset
}

// layoutRow assigns horizontal center positions to boxes so they are
// evenly spaced across the available width.
func (r *DAGRenderer) layoutRow(row []*nodeBox, width int) {
	n := len(row)
	if n == 0 {
		return
	}

	// Total width needed by all boxes.
	totalBoxWidth := 0
	for _, b := range row {
		totalBoxWidth += b.width
	}

	// Gap between boxes.
	gap := 0
	if n > 1 && totalBoxWidth < width {
		gap = (width - totalBoxWidth) / (n + 1)
		if gap < 2 {
			gap = 2
		}
	}

	// Assign centers.
	x := gap
	for _, b := range row {
		b.center = x + b.width/2
		x += b.width + gap
	}

	// If only one box, center it.
	if n == 1 {
		row[0].center = width / 2
	}
}

// drawRow writes the box lines for a row of nodes into the builder.
func (r *DAGRenderer) drawRow(sb *strings.Builder, row []*nodeBox, width int) {
	if len(row) == 0 {
		return
	}

	// All boxes in a row should have the same number of lines.
	maxLines := 0
	for _, b := range row {
		if len(b.lines) > maxLines {
			maxLines = len(b.lines)
		}
	}

	for lineIdx := 0; lineIdx < maxLines; lineIdx++ {
		var lineBuf strings.Builder
		cursor := 0
		for _, b := range row {
			if lineIdx >= len(b.lines) {
				continue
			}
			startCol := b.center - b.width/2
			if startCol < 0 {
				startCol = 0
			}
			if startCol > cursor {
				lineBuf.WriteString(strings.Repeat(" ", startCol-cursor))
				cursor = startCol
			}
			lineBuf.WriteString(b.lines[lineIdx])
			cursor = startCol + r.visibleLen(b.lines[lineIdx])
		}
		sb.WriteString(lineBuf.String())
		sb.WriteByte('\n')
	}
}

// drawConnectors draws vertical and branching connectors between two
// adjacent waves.
func (r *DAGRenderer) drawConnectors(sb *strings.Builder, prevWave, currWave dag.Wave, boxes map[string]*nodeBox, deps map[string][]string, width int) {
	// Build a list of connections: (from-center, to-center).
	type connection struct {
		fromCenter int
		toCenter   int
	}
	var conns []connection

	for _, toID := range currWave.NodeIDs {
		toBox := boxes[toID]
		for _, depID := range deps[toID] {
			// Only draw if the dependency is in the previous wave.
			if !containsStr(prevWave.NodeIDs, depID) {
				continue
			}
			fromBox := boxes[depID]
			conns = append(conns, connection{
				fromCenter: fromBox.center,
				toCenter:   toBox.center,
			})
		}
	}

	if len(conns) == 0 {
		// Draw simple vertical connectors for nodes in currWave that
		// depend on anything in prevWave (multi-wave skip).
		for _, toID := range currWave.NodeIDs {
			for _, depID := range deps[toID] {
				fromBox := boxes[depID]
				if fromBox == nil {
					continue
				}
				toBox := boxes[toID]
				conns = append(conns, connection{
					fromCenter: fromBox.center,
					toCenter:   toBox.center,
				})
			}
		}
	}

	if len(conns) == 0 {
		return
	}

	// Render connector lines (2 lines: a down-line and a branching line).
	// Line 1: vertical drops from parent centers.
	line1 := make([]rune, width)
	for i := range line1 {
		line1[i] = ' '
	}
	for _, c := range conns {
		col := c.fromCenter
		if col >= 0 && col < width {
			line1[col] = '│'
		}
	}
	sb.WriteString(strings.TrimRight(string(line1), " "))
	sb.WriteByte('\n')

	// Line 2: horizontal branches + vertical arrivals.
	line2 := make([]rune, width)
	for i := range line2 {
		line2[i] = ' '
	}

	// Group connections by source to detect fan-out.
	type connGroup struct {
		from int
		tos  []int
	}
	fromMap := make(map[int][]int)
	for _, c := range conns {
		fromMap[c.fromCenter] = append(fromMap[c.fromCenter], c.toCenter)
	}

	// Sort fromMap keys for deterministic iteration order.
	fromKeys := make([]int, 0, len(fromMap))
	for k := range fromMap {
		fromKeys = append(fromKeys, k)
	}
	sort.Ints(fromKeys)

	for _, from := range fromKeys {
		tos := fromMap[from]
		if len(tos) == 1 && tos[0] == from {
			// Straight drop.
			if from >= 0 && from < width {
				line2[from] = '│'
			}
			continue
		}

		// Sort targets.
		sort.Ints(tos)
		minTo := tos[0]
		maxTo := tos[len(tos)-1]

		// Include the source in the range.
		lo := minTo
		if from < lo {
			lo = from
		}
		hi := maxTo
		if from > hi {
			hi = from
		}

		// Draw horizontal span.
		for col := lo; col <= hi && col < width; col++ {
			if col < 0 {
				continue
			}
			if line2[col] == ' ' {
				line2[col] = '─'
			}
		}

		// Source junction.
		if from >= 0 && from < width {
			if len(tos) > 1 || (len(tos) == 1 && tos[0] != from) {
				line2[from] = '┴'
			} else {
				line2[from] = '│'
			}
		}

		// Target junctions.
		for _, to := range tos {
			if to >= 0 && to < width {
				if to == lo {
					line2[to] = '├'
				} else if to == hi {
					line2[to] = '┤'
				} else {
					line2[to] = '┬'
				}
			}
		}
	}

	// Also handle fan-in: multiple sources going to one target.
	toMap := make(map[int][]int)
	for _, c := range conns {
		toMap[c.toCenter] = append(toMap[c.toCenter], c.fromCenter)
	}
	// Sort toMap keys for deterministic iteration order.
	toKeys := make([]int, 0, len(toMap))
	for k := range toMap {
		toKeys = append(toKeys, k)
	}
	sort.Ints(toKeys)

	for _, to := range toKeys {
		froms := toMap[to]
		if len(froms) <= 1 {
			continue
		}
		sort.Ints(froms)
		lo := froms[0]
		hi := froms[len(froms)-1]
		if to < lo {
			lo = to
		}
		if to > hi {
			hi = to
		}
		for col := lo; col <= hi && col < width; col++ {
			if col < 0 {
				continue
			}
			if line2[col] == ' ' {
				line2[col] = '─'
			}
		}
		if to >= 0 && to < width {
			line2[to] = '┬'
		}
	}

	sb.WriteString(strings.TrimRight(string(line2), " "))
	sb.WriteByte('\n')
}

// ────────────────────────── compact mode ──────────────────────────

func (r *DAGRenderer) renderCompact(waves []dag.Wave, deps map[string][]string, titles map[string]string, width int) string {
	var sb strings.Builder

	// Build reverse deps: for each node, which nodes depend on it.
	children := make(map[string][]string)
	for id, depList := range deps {
		for _, dep := range depList {
			children[dep] = append(children[dep], id)
		}
	}
	// Sort children for determinism.
	for k := range children {
		sort.Strings(children[k])
	}

	// Assign each node to its wave number for labeling.
	nodeWave := make(map[string]int, len(titles))
	for _, w := range waves {
		for _, id := range w.NodeIDs {
			nodeWave[id] = w.Number
		}
	}

	for wi, w := range waves {
		if wi > 0 {
			sb.WriteByte('\n')
		}
		waveLabel := fmt.Sprintf("Wave %d: ", w.Number)
		sb.WriteString(r.applyColor(waveLabel, ansi.Dim))

		for ni, id := range w.NodeIDs {
			if ni > 0 {
				sb.WriteString(strings.Repeat(" ", len(waveLabel)))
			}
			title := titles[id]
			if title == "" {
				title = id
			}

			status := NodeStatus{State: "queued"}
			if r.StatusFunc != nil {
				status = r.StatusFunc(id)
			}

			nodeStr := r.compactNode(title, id, status.State)
			sb.WriteString(nodeStr)

			// Show immediate children as arrows.
			if ch, ok := children[id]; ok {
				for ci, childID := range ch {
					childTitle := titles[childID]
					if childTitle == "" {
						childTitle = childID
					}
					childStatus := NodeStatus{State: "queued"}
					if r.StatusFunc != nil {
						childStatus = r.StatusFunc(childID)
					}
					arrow := " → "
					sb.WriteString(arrow)
					sb.WriteString(r.compactNode(childTitle, childID, childStatus.State))
					if ci < len(ch)-1 {
						sb.WriteByte('\n')
						sb.WriteString(strings.Repeat(" ", len(waveLabel)+len("[")+len(title)+len("]")))
					}
				}
			}
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}

// compactNode renders a single node in compact form: [title].
func (r *DAGRenderer) compactNode(title, id, state string) string {
	text := "[" + title + "]"
	if !r.UseColor {
		if r.CriticalPath[id] {
			return "[" + title + "]*"
		}
		return text
	}

	var prefix string
	switch state {
	case "done":
		prefix = ansi.Green
	case "running":
		prefix = ansi.Yellow
	case "failed":
		prefix = ansi.Red
	case "blocked":
		prefix = ansi.Magenta
	default:
		prefix = ansi.Blue
	}
	if r.CriticalPath[id] {
		prefix = ansi.Bold + prefix
	}
	return prefix + text + ansi.Reset
}

// applyColor wraps text with the given ANSI code if UseColor is true.
func (r *DAGRenderer) applyColor(text, code string) string {
	if !r.UseColor {
		return text
	}
	return code + text + ansi.Reset
}

// visibleLen returns the visible length of a string, stripping ANSI escapes.
func (r *DAGRenderer) visibleLen(s string) int {
	n := 0
	inEscape := false
	for _, c := range s {
		if c == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
				inEscape = false
			}
			continue
		}
		n++
	}
	return n
}

// containsStr reports whether ss contains s.
func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

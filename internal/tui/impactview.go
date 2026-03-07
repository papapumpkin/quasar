package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// FileImpact represents the aggregate impact on a single file across phases.
type FileImpact struct {
	Path      string
	Change    ChangeType
	Additions int
	Deletions int
	PhaseIDs  []string // phases that touch this file
}

// PhaseFileGroup holds a phase ID and its associated file impacts.
type PhaseFileGroup struct {
	PhaseID string
	Files   []FileImpact
}

// ImpactView renders a scrollable view of aggregate file impact across phases.
// Files touched by multiple phases are highlighted as overlaps.
type ImpactView struct {
	groups     []PhaseFileGroup
	fileMap    map[string][]string // path → list of phase IDs (for overlap detection)
	viewport   viewport.Model
	width      int
	height     int
	totalLines int
	ready      bool
	cursor     int
}

// NewImpactView creates an empty impact view.
func NewImpactView() ImpactView {
	return ImpactView{
		fileMap: make(map[string][]string),
	}
}

// SetSize updates the viewport dimensions and re-renders content.
func (iv *ImpactView) SetSize(width, height int) {
	iv.width = width
	iv.height = height
	if !iv.ready {
		iv.viewport = viewport.New(width, height)
		iv.ready = true
	} else {
		iv.viewport.Width = width
		iv.viewport.Height = height
	}
	iv.refreshContent()
}

// Rebuild reconstructs the impact view from the current set of worker cards
// and any completed phase file data. It accepts a map of phaseID → file claims
// with change type and stat information.
func (iv *ImpactView) Rebuild(phaseFiles map[string][]FileStatEntry) {
	iv.fileMap = make(map[string][]string)
	iv.groups = nil

	// Sort phase IDs for stable ordering.
	phaseIDs := make([]string, 0, len(phaseFiles))
	for pid := range phaseFiles {
		phaseIDs = append(phaseIDs, pid)
	}
	sort.Strings(phaseIDs)

	for _, pid := range phaseIDs {
		files := phaseFiles[pid]
		if len(files) == 0 {
			continue
		}
		group := PhaseFileGroup{PhaseID: pid}
		for _, f := range files {
			impact := FileImpact{
				Path:      f.Path,
				Change:    f.Change,
				Additions: f.Additions,
				Deletions: f.Deletions,
				PhaseIDs:  []string{pid},
			}
			group.Files = append(group.Files, impact)

			// Track cross-phase overlap.
			iv.fileMap[f.Path] = appendUnique(iv.fileMap[f.Path], pid)
		}
		iv.groups = append(iv.groups, group)
	}
	iv.refreshContent()
}

// appendUnique appends s to the slice if not already present.
func appendUnique(slice []string, s string) []string {
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}
	return append(slice, s)
}

// Overlaps returns the set of file paths touched by more than one phase.
func (iv *ImpactView) Overlaps() map[string][]string {
	result := make(map[string][]string)
	for path, phases := range iv.fileMap {
		if len(phases) > 1 {
			result[path] = phases
		}
	}
	return result
}

// OverlapCount returns the number of files with cross-phase overlap.
func (iv *ImpactView) OverlapCount() int {
	n := 0
	for _, phases := range iv.fileMap {
		if len(phases) > 1 {
			n++
		}
	}
	return n
}

// TotalFiles returns the total number of unique files tracked.
func (iv *ImpactView) TotalFiles() int {
	return len(iv.fileMap)
}

// MoveUp moves the cursor up.
func (iv *ImpactView) MoveUp() {
	if iv.cursor > 0 {
		iv.cursor--
	}
	iv.refreshContent()
}

// MoveDown moves the cursor down.
func (iv *ImpactView) MoveDown() {
	max := len(iv.groups) - 1
	if max < 0 {
		max = 0
	}
	if iv.cursor < max {
		iv.cursor++
	}
	iv.refreshContent()
}

// ClampCursor ensures the cursor is within bounds.
func (iv *ImpactView) ClampCursor() {
	total := len(iv.groups)
	if total == 0 {
		iv.cursor = 0
		return
	}
	if iv.cursor >= total {
		iv.cursor = total - 1
	}
	if iv.cursor < 0 {
		iv.cursor = 0
	}
}

// Update handles viewport scroll key events.
func (iv *ImpactView) Update(msg tea.Msg) {
	if !iv.ready {
		return
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "home", "g":
			iv.viewport.GotoTop()
			return
		case "end", "G":
			iv.viewport.GotoBottom()
			return
		}
	}
	iv.viewport, _ = iv.viewport.Update(msg)
}

// View renders the impact view or an empty placeholder.
func (iv ImpactView) View() string {
	if len(iv.groups) == 0 {
		return lipgloss.NewStyle().
			Foreground(colorMuted).
			PaddingLeft(2).
			Render("No file impacts")
	}
	if !iv.ready {
		return ""
	}
	return iv.viewport.View()
}

// refreshContent re-renders all content into the viewport.
func (iv *ImpactView) refreshContent() {
	if !iv.ready {
		return
	}
	content := iv.renderContent()
	iv.totalLines = strings.Count(content, "\n") + 1
	iv.viewport.SetContent(content)
}

// renderContent formats all phase file groups into a single string.
func (iv ImpactView) renderContent() string {
	if len(iv.groups) == 0 {
		return ""
	}

	overlaps := iv.Overlaps()

	// Summary header.
	summaryStyle := lipgloss.NewStyle().Foreground(colorMutedLight)
	overlapCount := len(overlaps)
	summaryText := fmt.Sprintf("  %d files across %d phases", iv.TotalFiles(), len(iv.groups))
	if overlapCount > 0 {
		warnStyle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		noun := "overlaps"
		if overlapCount == 1 {
			noun = "overlap"
		}
		summaryText += "  " + warnStyle.Render(fmt.Sprintf("%d %s", overlapCount, noun))
	}

	var sb strings.Builder
	sb.WriteString(summaryStyle.Render(summaryText))
	sb.WriteString("\n\n")

	for gi, group := range iv.groups {
		selected := gi == iv.cursor

		// Phase header with selection indicator.
		sb.WriteString(renderPhaseGroupHeader(group.PhaseID, selected))
		sb.WriteString("\n")

		// File entries.
		for _, fi := range group.Files {
			_, isOverlap := overlaps[fi.Path]
			sb.WriteString(renderFileImpactLine(fi, isOverlap, iv.width-6))
			sb.WriteString("\n")
		}

		if gi < len(iv.groups)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// renderPhaseGroupHeader renders the header for a phase group.
func renderPhaseGroupHeader(phaseID string, selected bool) string {
	if selected {
		indStyle := lipgloss.NewStyle().Foreground(colorBlueshift).Bold(true)
		nameStyle := lipgloss.NewStyle().Foreground(colorBlueshift).Bold(true)
		return indStyle.Render("  "+selectionIndicator+" ") + nameStyle.Render(phaseID)
	}
	nameStyle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	return "    " + nameStyle.Render(phaseID)
}

// renderFileImpactLine renders a single file line with change type glyph, stat, and overlap indicator.
func renderFileImpactLine(fi FileImpact, overlap bool, maxWidth int) string {
	var b strings.Builder

	b.WriteString("      ")

	// Change type glyph with appropriate color.
	glyph := ChangeTypeGlyph(fi.Change)
	glyphStyle := changeGlyphStyle(fi.Change)
	b.WriteString(glyphStyle.Render(glyph))
	b.WriteString(" ")

	// File path.
	pathStyle := lipgloss.NewStyle().Foreground(colorWhite)
	if overlap {
		pathStyle = lipgloss.NewStyle().Foreground(colorStarYellow)
	}
	path := fi.Path
	statStr := formatFileStat(fi.Additions, fi.Deletions)
	// Reserve space for stat + overlap indicator.
	overheadLen := len(statStr) + 4
	if overlap {
		overheadLen += 4 // " !!"
	}
	availPath := maxWidth - overheadLen
	if availPath < 10 {
		availPath = 10
	}
	if len(path) > availPath {
		path = TruncateWithEllipsis(path, availPath)
	}
	b.WriteString(pathStyle.Render(path))

	// Stat.
	b.WriteString("  ")
	b.WriteString(statStr)

	// Overlap warning.
	if overlap {
		warnStyle := lipgloss.NewStyle().Foreground(colorDanger).Bold(true)
		b.WriteString(" ")
		b.WriteString(warnStyle.Render("!!"))
	}

	return b.String()
}

// changeGlyphStyle returns the lipgloss style for a change type glyph.
func changeGlyphStyle(ct ChangeType) lipgloss.Style {
	switch ct {
	case ChangeAdded:
		return styleDiffChangeAdded
	case ChangeDeleted:
		return styleDiffChangeDeleted
	case ChangeRenamed:
		return styleDiffChangeRenamed
	default:
		return styleDiffChangeModified
	}
}

// formatFileStat formats additions/deletions as a compact stat string.
func formatFileStat(additions, deletions int) string {
	if additions == 0 && deletions == 0 {
		return ""
	}
	var parts []string
	if additions > 0 {
		parts = append(parts, styleDiffStatAdd.Render(fmt.Sprintf("+%d", additions)))
	}
	if deletions > 0 {
		parts = append(parts, styleDiffStatDel.Render(fmt.Sprintf("-%d", deletions)))
	}
	return strings.Join(parts, " ")
}

// FileScopeSection renders a "scope" section for a phase's file list,
// suitable for embedding in the detail panel header.
func FileScopeSection(files []FileStatEntry, width int) string {
	if len(files) == 0 {
		return ""
	}

	var b strings.Builder
	label := lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
	b.WriteString(label.Render("scope:"))
	b.WriteString("\n")

	for _, f := range files {
		glyph := ChangeTypeGlyph(f.Change)
		glyphStyle := changeGlyphStyle(f.Change)
		pathStyle := lipgloss.NewStyle().Foreground(colorMutedLight)

		// Truncate long paths to fit the available width.
		// Reserve: 2 (indent) + 1 (glyph) + 1 (space) + ~12 (stat) = ~16 overhead.
		path := f.Path
		availPath := width - 16
		if availPath < 10 {
			availPath = 10
		}
		if len(path) > availPath {
			path = TruncateWithEllipsis(path, availPath)
		}

		line := "  " + glyphStyle.Render(glyph) + " " + pathStyle.Render(path)
		stat := formatFileStat(f.Additions, f.Deletions)
		if stat != "" {
			line += "  " + stat
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

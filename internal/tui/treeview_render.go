package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Tree connector characters with trailing space for the tree view.
// treeConnectorMid and treeConnectorLast (without trailing space) are in beadview.go.
const (
	tvConnectorMid  = "├─ "
	tvConnectorLast = "└─ "
	tvConnectorPipe = "│  "
)

// View renders the tree view as a bordered panel for the sidebar.
func (tv TreeView) View() string {
	if tv.Width < 4 || tv.Height < 1 {
		return ""
	}

	// Title.
	titleStyle := lipgloss.NewStyle().
		Foreground(colorPrimary).
		Bold(true)
	title := titleStyle.Render("PHASES")

	// Inner width excludes border (2) and padding (2).
	innerWidth := tv.Width - 4
	if innerWidth < 4 {
		innerWidth = 4
	}

	var rows []string
	rows = append(rows, title)
	rows = append(rows, "") // spacing after title

	if len(tv.FlatNodes) == 0 {
		empty := lipgloss.NewStyle().Foreground(colorMuted).Render("  (no phases)")
		rows = append(rows, empty)
	} else {
		// Available lines for tree entries (height minus title and spacing).
		visibleLines := tv.Height - 2
		if visibleLines < 1 {
			visibleLines = 1
		}

		// Top scroll indicator.
		if tv.Offset > 0 {
			indicator := lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
				Render(fmt.Sprintf("  ↑ %d more", tv.Offset))
			rows = append(rows, indicator)
			visibleLines--
		}

		// Reserve a line for bottom indicator if needed.
		remaining := len(tv.FlatNodes) - tv.Offset
		hasBottomIndicator := remaining > visibleLines
		if hasBottomIndicator {
			visibleLines--
		}

		for i := tv.Offset; i < len(tv.FlatNodes) && i < tv.Offset+visibleLines; i++ {
			node := tv.FlatNodes[i]
			row := tv.renderTreeRow(node, i == tv.Cursor, innerWidth)
			rows = append(rows, row)
		}

		// Bottom scroll indicator.
		if hasBottomIndicator {
			belowCount := len(tv.FlatNodes) - (tv.Offset + visibleLines)
			indicator := lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
				Render(fmt.Sprintf("  ↓ %d more", belowCount))
			rows = append(rows, indicator)
		}
	}

	content := strings.Join(rows, "\n")

	borderStyle := panelStyle(tv.Focus).
		Width(tv.Width - 2). // subtract border width
		Height(tv.Height)

	return borderStyle.Render(content)
}

// renderTreeRow renders a single node row with tree-drawing characters.
func (tv TreeView) renderTreeRow(node *TreeNode, selected bool, maxWidth int) string {
	// Build indent as two parts: a plain-text margin prefix and styled
	// tree connector suffix. This avoids slicing into ANSI escape codes
	// when replacing the margin with the selection indicator.
	indentMargin, indentConnector := tv.buildIndentParts(node)

	// Status icon.
	iconStr := ""
	if node.Status != "" {
		iconStr = tv.styleNodeIcon(node) + " "
	}

	// Expand/collapse indicator for nodes with children.
	expandIndicator := ""
	if len(node.Children) > 0 {
		if node.Expanded {
			expandIndicator = "▾ "
		} else {
			expandIndicator = "▸ "
		}
	}

	// Label — truncate to fit.
	label := node.Label

	// Calculate space used by non-label elements.
	indent := indentMargin + indentConnector
	indentWidth := lipgloss.Width(indent)
	iconWidth := lipgloss.Width(iconStr)
	expandWidth := lipgloss.Width(expandIndicator)

	metricsStr := ""
	metricsWidth := 0
	if node.Metrics != "" && node.Kind != TreeNodeNebula {
		metricsStr = node.Metrics
		metricsWidth = lipgloss.Width(metricsStr) + 1 // +1 for spacing
	}

	labelWidth := maxWidth - indentWidth - iconWidth - expandWidth - metricsWidth
	if labelWidth < 4 {
		labelWidth = 4
	}
	label = TruncateWithEllipsis(label, labelWidth)

	// Build the row.
	var row string
	if selected {
		indicator := styleSelectionIndicator.Render(selectionIndicator)
		styledLabel := styleRowSelected.Render(label)
		styledExpand := lipgloss.NewStyle().Foreground(colorPrimary).Render(expandIndicator)
		// Replace the plain-text margin with the selection indicator,
		// keeping the ANSI-styled connector suffix intact.
		row = indicator + indentConnector + iconStr + styledExpand + styledLabel
	} else {
		styledLabel := tv.styleLabelForNode(node, label)
		styledExpand := lipgloss.NewStyle().Foreground(colorMuted).Render(expandIndicator)
		row = indent + iconStr + styledExpand + styledLabel
	}

	// Append right-aligned metrics.
	if metricsStr != "" {
		rowWidth := lipgloss.Width(row)
		gap := maxWidth - rowWidth - lipgloss.Width(metricsStr)
		if gap < 1 {
			gap = 1
		}
		styledMetrics := stylePhaseDetail.Render(metricsStr)
		row += strings.Repeat(" ", gap) + styledMetrics
	}

	// Pad and highlight selected row.
	if selected {
		visible := lipgloss.Width(row)
		if visible < maxWidth {
			row += strings.Repeat(" ", maxWidth-visible)
		}
		row = lipgloss.NewStyle().Background(colorSelectionBg).Render(row)
	}

	return row
}

// buildIndentParts returns the tree indent as two separate strings:
// a plain-text margin (always "  ") and a styled connector suffix
// containing ANSI-escaped tree-drawing characters. This separation
// allows the selection indicator to replace the margin without
// slicing into ANSI escape codes.
func (tv TreeView) buildIndentParts(node *TreeNode) (margin, connector string) {
	margin = "  " // base margin, always plain ASCII
	if node.Depth == 0 {
		return margin, ""
	}
	var b strings.Builder
	for i := 1; i < node.Depth; i++ {
		b.WriteString(styleTreeConnector.Render(tvConnectorPipe))
	}
	// Determine if this is the last child of its parent.
	if tv.isLastChild(node) {
		b.WriteString(styleTreeConnector.Render(tvConnectorLast))
	} else {
		b.WriteString(styleTreeConnector.Render(tvConnectorMid))
	}
	return margin, b.String()
}

// isLastChild checks if a node is the last child of its parent.
func (tv TreeView) isLastChild(node *TreeNode) bool {
	parent := tv.findParent(node)
	if parent == nil || len(parent.Children) == 0 {
		return true
	}
	return parent.Children[len(parent.Children)-1] == node
}

// styleNodeIcon returns a styled status icon for the node.
func (tv TreeView) styleNodeIcon(node *TreeNode) string {
	switch node.Status {
	case iconDone:
		return styleRowDone.Render(node.Status)
	case iconFailed:
		return styleRowFailed.Render(node.Status)
	case iconWorking:
		return styleRowWorking.Render(node.Status)
	case iconGate:
		return styleRowGate.Render(node.Status)
	case iconSkipped:
		return styleRowWaiting.Render(node.Status)
	default:
		return styleRowWaiting.Render(node.Status)
	}
}

// styleLabelForNode returns a styled label based on node kind and status.
func (tv TreeView) styleLabelForNode(node *TreeNode, label string) string {
	switch node.Kind {
	case TreeNodeNebula:
		return lipgloss.NewStyle().Foreground(colorPrimary).Bold(true).Render(label)
	case TreeNodePhase:
		if node.PhaseEntry != nil {
			return phaseStatusStyle(node.PhaseEntry.Status).Render(label)
		}
		return styleRowNormal.Render(label)
	case TreeNodeCycle:
		return styleRowNormal.Render(label)
	case TreeNodeAgent:
		if node.AgentEntry != nil && node.AgentEntry.Done {
			return stylePhaseID.Render(label)
		}
		return styleRowWorking.Render(label)
	default:
		return styleRowNormal.Render(label)
	}
}

package tui

import "github.com/charmbracelet/lipgloss"

// Color palette.
var (
	colorCyan    = lipgloss.Color("#00BFFF")
	colorBlue    = lipgloss.Color("#5B8DEF")
	colorYellow  = lipgloss.Color("#FFD700")
	colorGreen   = lipgloss.Color("#00FF87")
	colorRed     = lipgloss.Color("#FF5F5F")
	colorMagenta = lipgloss.Color("#FF87FF")
	colorDim     = lipgloss.Color("#666666")
	colorWhite   = lipgloss.Color("#FFFFFF")
)

// Status bar styles.
var (
	styleStatusBar = lipgloss.NewStyle().
			Background(lipgloss.Color("#333333")).
			Foreground(colorWhite).
			Bold(true).
			Padding(0, 1)

	styleStatusLabel = lipgloss.NewStyle().
				Foreground(colorCyan).
				Bold(true)

	styleStatusValue = lipgloss.NewStyle().
				Foreground(colorWhite)

	styleStatusCost = lipgloss.NewStyle().
			Foreground(colorYellow)
)

// Phase/cycle row styles.
var (
	styleRowSelected = lipgloss.NewStyle().
				Foreground(colorWhite).
				Bold(true)

	styleRowNormal = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AAAAAA"))

	styleRowDone = lipgloss.NewStyle().
			Foreground(colorGreen)

	styleRowWorking = lipgloss.NewStyle().
			Foreground(colorBlue)

	styleRowFailed = lipgloss.NewStyle().
			Foreground(colorRed)

	styleRowGate = lipgloss.NewStyle().
			Foreground(colorYellow)

	styleRowWaiting = lipgloss.NewStyle().
			Foreground(colorDim)
)

// Detail panel styles.
var (
	styleDetailBorder = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(colorDim).
				Padding(0, 1)

	styleDetailTitle = lipgloss.NewStyle().
				Foreground(colorCyan).
				Bold(true)

	styleDetailDim = lipgloss.NewStyle().
			Foreground(colorDim)
)

// Gate prompt styles.
var (
	styleGateOverlay = lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(colorYellow).
				Padding(1, 2).
				Bold(true)

	styleGateAction = lipgloss.NewStyle().
			Foreground(colorYellow).
			Bold(true)

	styleGateSelected = lipgloss.NewStyle().
				Foreground(colorWhite).
				Background(colorYellow).
				Bold(true).
				Padding(0, 1)

	styleGateNormal = lipgloss.NewStyle().
			Foreground(colorDim).
			Padding(0, 1)
)

// Footer styles.
var (
	styleFooter = lipgloss.NewStyle().
			Foreground(colorDim)

	styleFooterKey = lipgloss.NewStyle().
			Foreground(colorWhite).
			Bold(true)

	styleFooterSep = lipgloss.NewStyle().
			Foreground(colorDim)
)

// Section border for separating view regions.
var (
	styleSectionBorder = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), true, false, false, false).
				BorderForeground(colorDim)
)

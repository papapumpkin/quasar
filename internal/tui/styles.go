package tui

import "github.com/charmbracelet/lipgloss"

// Semantic color palette.
var (
	colorPrimary       = lipgloss.Color("#00BFFF") // Cyan — primary accent
	colorAccent        = lipgloss.Color("#FFD700") // Gold — attention/gate
	colorSuccess       = lipgloss.Color("#00E676") // Green — completed
	colorDanger        = lipgloss.Color("#FF5252") // Red — errors/failures
	colorMuted         = lipgloss.Color("#636363") // Gray — de-emphasized
	colorMutedLight    = lipgloss.Color("#8C8C8C") // Lighter gray — normal text
	colorWhite         = lipgloss.Color("#EEEEEE") // Off-white — primary text
	colorBrightWhite   = lipgloss.Color("#FFFFFF") // Pure white — emphatic text
	colorSurface       = lipgloss.Color("#1E1E2E") // Dark surface — status bar bg
	colorSurfaceBright = lipgloss.Color("#2A2A3C") // Lighter surface — breadcrumb bg
	colorSurfaceDim    = lipgloss.Color("#181825") // Darkest surface — footer bg
	colorBlue          = lipgloss.Color("#5B8DEF") // Blue — working/active
)

// Selection indicator prepended to the active row.
const selectionIndicator = "▎"

// Status icons for phase/agent states.
const (
	iconDone    = "✓"
	iconFailed  = "✗"
	iconWorking = "◎"
	iconWaiting = "·"
	iconGate    = "⊘"
	iconSkipped = "–"
)

// Status bar styles — visually dominant with solid background.
var (
	styleStatusBar = lipgloss.NewStyle().
			Background(colorSurface).
			Foreground(colorWhite).
			Bold(true).
			Padding(0, 1)

	styleStatusLabel = lipgloss.NewStyle().
				Foreground(colorPrimary).
				Bold(true)

	styleStatusValue = lipgloss.NewStyle().
				Foreground(colorWhite)

	styleStatusCost = lipgloss.NewStyle().
			Foreground(colorAccent)
)

// Breadcrumb bar style — subtle tinted background, dimmer than status bar.
var styleBreadcrumb = lipgloss.NewStyle().
	Background(colorSurfaceBright).
	Foreground(colorMutedLight).
	Padding(0, 1)

// styleBreadcrumbSep styles the separator between breadcrumb segments.
var styleBreadcrumbSep = lipgloss.NewStyle().
	Foreground(colorMuted)

// Phase/cycle row styles.
var (
	styleRowSelected = lipgloss.NewStyle().
				Foreground(colorBrightWhite).
				Bold(true)

	styleRowNormal = lipgloss.NewStyle().
			Foreground(colorMutedLight)

	styleRowDone = lipgloss.NewStyle().
			Foreground(colorSuccess)

	styleRowWorking = lipgloss.NewStyle().
			Foreground(colorBlue)

	styleRowFailed = lipgloss.NewStyle().
			Foreground(colorDanger).
			Bold(true)

	styleRowGate = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	styleRowWaiting = lipgloss.NewStyle().
			Foreground(colorMuted)

	// styleSelectionIndicator styles the left-edge indicator for the selected row.
	styleSelectionIndicator = lipgloss.NewStyle().
				Foreground(colorPrimary).
				Bold(true)
)

// Detail panel styles — rounded border, styled title.
var (
	styleDetailBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorMuted).
				Padding(0, 1)

	styleDetailTitle = lipgloss.NewStyle().
				Foreground(colorPrimary).
				Bold(true)

	styleDetailDim = lipgloss.NewStyle().
			Foreground(colorMuted)
)

// Gate prompt styles.
var (
	styleGateOverlay = lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(colorAccent).
				Padding(1, 2).
				Bold(true)

	styleGateAction = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	styleGateSelected = lipgloss.NewStyle().
				Foreground(colorBrightWhite).
				Background(colorAccent).
				Bold(true).
				Padding(0, 1)

	styleGateNormal = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1)
)

// Footer styles — top border, clear key/desc contrast.
var (
	styleFooter = lipgloss.NewStyle().
			Foreground(colorMuted).
			Background(colorSurfaceDim).
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(colorMuted)

	styleFooterKey = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	styleFooterSep = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleFooterDesc = lipgloss.NewStyle().
			Foreground(colorMutedLight)
)

// Section border for separating view regions.
var styleSectionBorder = lipgloss.NewStyle().
	Border(lipgloss.NormalBorder(), true, false, false, false).
	BorderForeground(colorMuted)

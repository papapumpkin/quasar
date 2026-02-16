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
	colorBudgetWarn    = lipgloss.Color("#FFA726") // Orange — budget warning
	colorReviewer      = lipgloss.Color("#FFD54F") // Yellow — reviewer spinner
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

	styleStatusPaused = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	styleStatusStopping = lipgloss.NewStyle().
				Foreground(colorDanger).
				Bold(true)
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

	// styleDetailHeader styles the contextual header block above content.
	styleDetailHeader = lipgloss.NewStyle().
				Foreground(colorMutedLight)

	// styleDetailHeaderLabel styles labels in the header (e.g. "role:", "cost:").
	styleDetailHeaderLabel = lipgloss.NewStyle().
				Foreground(colorPrimary).
				Bold(true)

	// styleDetailHeaderValue styles values in the header.
	styleDetailHeaderValue = lipgloss.NewStyle().
				Foreground(colorWhite)

	// styleHighlightApproved styles "APPROVED" matches in agent output.
	styleHighlightApproved = lipgloss.NewStyle().
				Foreground(colorSuccess).
				Bold(true)

	// styleHighlightIssue styles "ISSUE:" matches in agent output.
	styleHighlightIssue = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	// styleHighlightCritical styles "SEVERITY: critical" matches in agent output.
	styleHighlightCritical = lipgloss.NewStyle().
				Foreground(colorDanger).
				Bold(true)

	// styleScrollIndicator styles the scroll up/down hints.
	styleScrollIndicator = lipgloss.NewStyle().
				Foreground(colorMuted).
				Italic(true)

	// styleDetailSep styles the separator between header and body.
	styleDetailSep = lipgloss.NewStyle().
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

// Completion overlay styles.
var (
	styleOverlaySuccess = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorSuccess).
				Padding(1, 3)

	styleOverlayWarning = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorAccent).
				Padding(1, 3)

	styleOverlayError = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorDanger).
				Padding(1, 3)

	styleOverlayTitle = lipgloss.NewStyle().
				Bold(true)

	styleOverlayHint = lipgloss.NewStyle().
				Foreground(colorMuted).
				Italic(true)

	styleOverlayDimmed = lipgloss.NewStyle().
				Foreground(colorMuted)
)

// Toast notification styles.
var styleToast = lipgloss.NewStyle().
	Background(colorDanger).
	Foreground(colorBrightWhite).
	Bold(true).
	Padding(0, 1)

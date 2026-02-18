package tui

import "github.com/charmbracelet/lipgloss"

// Semantic color palette — galactic theme.
var (
	colorPrimary       = lipgloss.Color("#58A6FF") // Starlight blue — primary accent
	colorAccent        = lipgloss.Color("#FFA657") // Supernova orange — attention/gate
	colorSuccess       = lipgloss.Color("#00E676") // Green — completed (unchanged)
	colorDanger        = lipgloss.Color("#FF7B72") // Supernova red — errors/failures
	colorMuted         = lipgloss.Color("#484F58") // Space dust gray — de-emphasized
	colorMutedLight    = lipgloss.Color("#8B949E") // Cosmic dust — normal text
	colorWhite         = lipgloss.Color("#E6EDF3") // Distant starlight — primary text
	colorBrightWhite   = lipgloss.Color("#FFFFFF") // Pure white — emphatic text
	colorSurface       = lipgloss.Color("#1A1A40") // Deep indigo — status bar bg (clearly tinted)
	colorSurfaceBright = lipgloss.Color("#161B22") // Nebula dust — breadcrumb bg
	colorSurfaceDim    = lipgloss.Color("#080B10") // Void black — footer bg
	colorBlue          = lipgloss.Color("#79C0FF") // Stellar blue — working/active
	colorBudgetWarn    = lipgloss.Color("#FFA657") // Supernova orange — budget warning
	colorReviewer      = lipgloss.Color("#E3B341") // Star yellow — reviewer spinner
	colorStarYellow    = lipgloss.Color("#E3B341") // Star yellow — highlights, sparkle accents
	colorNebula        = lipgloss.Color("#BC8CFF") // Nebula purple — breadcrumbs, secondary UI
	colorNebulaDeep    = lipgloss.Color("#8B5CF6") // Deep nebula — selected backgrounds, borders
	colorRedshift      = lipgloss.Color("#FF6B6B") // Warm red — Doppler left jet
	colorBlueshift     = lipgloss.Color("#4FC3F7") // Cool cyan — Doppler right jet
	colorSelectionBg   = lipgloss.Color("#2D2D5E") // Dim nebula tint — selected row background
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
			Foreground(colorMutedLight).
			Padding(0, 1)

	// styleStatusMode renders mode labels ("nebula:", "task") in a dimmer secondary color.
	styleStatusMode = lipgloss.NewStyle().
			Background(colorSurface).
			Foreground(colorMutedLight)

	// styleStatusName renders the task/nebula name — bold for emphasis but same bar foreground.
	styleStatusName = lipgloss.NewStyle().
			Background(colorSurface).
			Foreground(colorMutedLight).
			Bold(true)

	// styleStatusProgress renders progress text in the uniform bar foreground.
	styleStatusProgress = lipgloss.NewStyle().
				Background(colorSurface).
				Foreground(colorMutedLight)

	// styleStatusProgressActive renders progress text when some items are completed (same uniform color).
	styleStatusProgressActive = lipgloss.NewStyle().
					Background(colorSurface).
					Foreground(colorMutedLight)

	styleStatusCost = lipgloss.NewStyle().
			Background(colorSurface).
			Foreground(colorMutedLight)

	// styleStatusElapsed renders the elapsed time in the uniform bar foreground.
	styleStatusElapsed = lipgloss.NewStyle().
				Background(colorSurface).
				Foreground(colorMutedLight)

	styleStatusPaused = lipgloss.NewStyle().
				Background(colorSurface).
				Foreground(colorAccent).
				Bold(true)

	styleStatusStopping = lipgloss.NewStyle().
				Background(colorSurface).
				Foreground(colorDanger).
				Bold(true)
)

// Breadcrumb bar style — subtle tinted background, dimmer than status bar.
var styleBreadcrumb = lipgloss.NewStyle().
	Background(colorSurfaceBright).
	Foreground(colorNebula).
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

	// stylePhaseID styles the phase name/ID — brighter and bold so it stands out from status detail.
	stylePhaseID = lipgloss.NewStyle().
			Foreground(colorWhite).
			Bold(true)

	// stylePhaseDetail styles status metadata (cycle count, elapsed, cost) — muted to stay subordinate.
	stylePhaseDetail = lipgloss.NewStyle().
				Foreground(colorMutedLight)

	// styleSelectionIndicator styles the left-edge indicator for the selected row.
	styleSelectionIndicator = lipgloss.NewStyle().
				Foreground(colorPrimary).
				Bold(true)
)

// styleTreeConnector styles the tree-drawing characters (├──, └──) in the cycle timeline.
var styleTreeConnector = lipgloss.NewStyle().
	Foreground(colorMuted)

// styleWaveHeader styles the wave separator lines in the nebula phase view.
var styleWaveHeader = lipgloss.NewStyle().
	Foreground(colorMuted)

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

// Diff view styles — side-by-side diff rendering.
var (
	// styleDiffAdd styles added lines with a green background.
	styleDiffAdd = lipgloss.NewStyle().
			Foreground(colorSuccess)

	// styleDiffRemove styles removed lines with a red foreground.
	styleDiffRemove = lipgloss.NewStyle().
			Foreground(colorDanger)

	// styleDiffContext styles unchanged context lines.
	styleDiffContext = lipgloss.NewStyle().
				Foreground(colorMutedLight)

	// styleDiffHeader styles file path headers in the diff.
	styleDiffHeader = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	// styleDiffLineNum styles line numbers in muted gray.
	styleDiffLineNum = lipgloss.NewStyle().
				Foreground(colorMuted)

	// styleDiffSep styles the column separator between left and right panes.
	styleDiffSep = lipgloss.NewStyle().
			Foreground(colorMuted)

	// styleDiffStat styles the stat summary line.
	styleDiffStat = lipgloss.NewStyle().
			Foreground(colorMutedLight)

	// styleDiffStatAdd styles the "+" portion of file stats.
	styleDiffStatAdd = lipgloss.NewStyle().
				Foreground(colorSuccess)

	// styleDiffStatDel styles the "-" portion of file stats.
	styleDiffStatDel = lipgloss.NewStyle().
				Foreground(colorDanger)
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

// Bead tracker styles.
var (
	// styleBeadOpen styles open beads (white ●).
	styleBeadOpen = lipgloss.NewStyle().
			Foreground(colorWhite)

	// styleBeadInProgress styles in-progress beads (blue ◎).
	styleBeadInProgress = lipgloss.NewStyle().
				Foreground(colorBlue)

	// styleBeadClosed styles closed beads (green ✓).
	styleBeadClosed = lipgloss.NewStyle().
			Foreground(colorSuccess)

	// styleBeadTitle styles bead titles.
	styleBeadTitle = lipgloss.NewStyle().
			Foreground(colorWhite)
)

// Resource indicator styles — uniform bar foreground for clean single-color bar.
var (
	// styleResourceNormal styles resource metrics in the uniform bar foreground.
	styleResourceNormal = lipgloss.NewStyle().
				Background(colorSurface).
				Foreground(colorMutedLight)

	// styleResourceWarning styles resource metrics in the uniform bar foreground.
	styleResourceWarning = lipgloss.NewStyle().
				Background(colorSurface).
				Foreground(colorMutedLight)

	// styleResourceDanger styles resource metrics in the uniform bar foreground.
	styleResourceDanger = lipgloss.NewStyle().
				Background(colorSurface).
				Foreground(colorMutedLight)
)

// Toast notification styles.
var styleToast = lipgloss.NewStyle().
	Background(colorDanger).
	Foreground(colorBrightWhite).
	Bold(true).
	Padding(0, 1)

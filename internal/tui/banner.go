package tui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
)

// BannerSize represents the size variant of the quasar ASCII art.
type BannerSize int

const (
	BannerNone BannerSize = iota // too narrow, hide entirely
	BannerXS                     // XS-A Pill: 34×6
	BannerS                      // S-A Wide Ellipse: 52×11
	BannerSB                     // S-B Tall Ellipse: 44×19 (side panel)
	BannerXL                     // XL Full Bleed: 90×27 (splash only)
)

// Side panel width: 44 art + 2 padding each side = 48.
const sidePanelWidth = 48

// artXS is the XS-A Pill variant (34 cols × 6 rows).
var artXS = []string{
	`       .·::··::···::··::·.`,
	`    .::\|/::··::··::\|/::·:.`,
	`   ::---@---::····::---@---::`,
	`    .::/|\ ::··::··::/|\::·:.`,
	`       .·::··::···::··::·.`,
	`       ···· Q U A S A R ····`,
}

// artSA is the S-A Wide Ellipse variant (52 cols × 10 rows).
var artSA = []string{
	`            ..·::··::····::··::·..`,
	`        .::··::··:.\|/.··./|\.::··::··::.`,
	`     .::··::··::·.·\|/.··./|\·.::··::··::.`,
	`   .::··::··::·.·---@---··---@---·.::··::··::.`,
	`  ::··::··::··::.·./|\.··.\|\.·.::··::··::··::`,
	`   .::··::··::·::··/|\ ··::\|\ ··::·::··::··::.`,
	`     .::··::··::·.·.|.·.··.|.·.::··::··::.`,
	`        .::··::··::··::····::··::··::··::.`,
	`            ..·::··::····::··::·..`,
	``,
	`                Q    U    A    S    A    R`,
}

// artSB is the S-B Tall Ellipse variant (44 cols × 19 rows).
var artSB = []string{
	`           ..·::····::·..`,
	`        .::··:::\|/:::··::.`,
	`      .::··::.··\|/··.::··::.`,
	`    .::··::. ·::\|/::· .::··::.`,
	`   ::··::. ··::.·|·.::·· .::··::`,
	`  ::··::. ··::---@---::·· .::··::`,
	`   ::··::. ··::.·|·.::·· .::··::`,
	`    .::··::. ·::/|\::· .::··::.`,
	`      .::··::..·/|\··..::··::.`,
	`    .::··::. ·::\|/::· .::··::.`,
	`   ::··::. ··::.·|·.::·· .::··::`,
	`  ::··::. ··::---@---::·· .::··::`,
	`   ::··::. ··::.·|·.::·· .::··::`,
	`    .::··::. ·::/|\::· .::··::.`,
	`      .::··::..·/|\··..::··::.`,
	`        .::··:::/|\:::··::.`,
	`           ..·::····::·..`,
	``,
	`          Q    U    A    S    A    R`,
}

// artXL is the XL Full Bleed variant (90 cols × 27 rows).
var artXL = []string{
	`                      .·::··::··::·.                    .·::··::··::·.`,
	`                  .::··::::::::::::··::.            .::··::::::::::::··::.`,
	`              .::··::::.    ··::||::··  ··::    ::··  ··::||::··    .::::··::.`,
	`           .::··:::.   ..··::::\||/::··    ··::··    ··::\||/::::··..   .:::··::.`,
	`        .::··:::.  ..··::::..   \||/  ..::··  ··::..  \||/   ..::::··..  .:::··::.`,
	`      .::··::. ..··::::..  ··::. \|/ .::··::····::··::. \|/ .::··  ..::::··.. .::··::.`,
	`    .::··::. .··::::. .··::.. ·:. | .:·::··:.  .::··::·. | .:· ..::··. .::::··. .::··::.`,
	`   .::·::. .··:::.  ··::.. ··::. | .::··  ::··::  ··::. | .::·· ..::··  .:::··. .::·::.`,
	`  .::·::. .··:::.  ··::. .··::.. | ..::··. .::··. .::··.. | ..::··. .::··  .:::··. .::·::.`,
	`  ::·::. .··:::.  ··::..··::. .  | . .::··:..::··:..::··. | . .::··..::··  .:::··. .::·::`,
	`  :·::..··:::.  ··::..··::..··:. | .::··..::····::..::··. | .:·..::··..::··  .:::··..::·:`,
	`  :·::.··:::. ··::..··::. ··::.. | ..::··. .:·::·:. .::··.. | ..::·· .::··..::·· .:::··.::·:`,
	`  :·:.··:::. ··::..··::..··::..---@---..::··..::::..::··..---@---..::··..::··..::·· .:::··.:·:`,
	`  :·::.··:::. ··::..··::. ··::.. | ..::··. .:·::·:. .::··.. | ..::·· .::··..::·· .:::··.::·:`,
	`  :·::..··:::.  ··::..··::..··:. | .::··..::····::..::··. | .:·..::··..::··  .:::··..::·:`,
	`  ::·::. .··:::.  ··::..··::. .  | . .::··:..::··:..::··. | . .::··..::··  .:::··. .::·::`,
	`  .::·::. .··:::.  ··::. .··::.. | ..::··. .::··. .::··.. | ..::··. .::··  .:::··. .::·::.`,
	`   .::·::. .··:::.  ··::.. ··::. | .::··  ::··::  ··::. | .::·· ..::··  .:::··. .::·::.`,
	`    .::··::. .··::::. .··::.. ·:. | .:·::··:.  .::··::·. | .:· ..::··. .::::··. .::··::.`,
	`      .::··::. ..··::::..  ··::. /|\ .::··::····::··::. /|\ .::··  ..::::··.. .::··::.`,
	`        .::··:::.  ..··::::..   /||\  ..::··  ··::..  /||\   ..::::··..  .:::··::.`,
	`           .::··:::.   ..··::::/||\ ::··    ··::··    ··::/||\::::··..   .:::··::.`,
	`              .::··::::.    ··::||::··  ··::    ::··  ··::||::··    .::::··::.`,
	`                  .::··::::::::::::··::.            .::··::::::::::::··::.`,
	`                      .·::··::··::·.                    .·::··::··::·.`,
	``,
	`                                   Q    U    A    S    A    R`,
}

// Lipgloss styles for Doppler shift coloring.
var (
	styleRedOuter  = lipgloss.NewStyle().Foreground(colorDanger)
	styleRedInner  = lipgloss.NewStyle().Foreground(colorRedshift)
	styleBlueOuter = lipgloss.NewStyle().Foreground(colorPrimary)
	styleBlueInner = lipgloss.NewStyle().Foreground(colorBlueshift)
	styleCore      = lipgloss.NewStyle().Foreground(colorStarYellow).Bold(true)
	styleFade      = lipgloss.NewStyle().Foreground(colorMuted)
)

// Banner holds terminal dimensions and provides styled quasar ASCII art views.
type Banner struct {
	Width  int
	Height int
}

// renderCache stores pre-rendered banner output keyed by size and width.
var (
	renderCache   = make(map[bannerCacheKey]string)
	renderCacheMu sync.Mutex
)

type bannerCacheKey struct {
	size   BannerSize
	width  int
	height int
}

// Size returns the appropriate BannerSize for the current dimensions.
func (b Banner) Size() BannerSize {
	switch {
	case b.Width >= 120:
		return BannerSB
	case b.Width >= 90:
		return BannerS
	case b.Width >= 60:
		return BannerXS
	default:
		return BannerNone
	}
}

// View returns the styled ASCII art as a top banner, centered horizontally.
// Returns "" for BannerSB (use SidePanelView instead) and BannerNone.
func (b Banner) View() string {
	size := b.Size()
	if size == BannerNone || size == BannerSB {
		return ""
	}

	key := bannerCacheKey{size: size, width: b.Width}
	renderCacheMu.Lock()
	if cached, ok := renderCache[key]; ok {
		renderCacheMu.Unlock()
		return cached
	}
	renderCacheMu.Unlock()

	var art []string
	switch size {
	case BannerXS:
		art = artXS
	case BannerS:
		art = artSA
	default:
		return ""
	}

	result := renderDopplerArt(art, b.Width, size)

	renderCacheMu.Lock()
	renderCache[key] = result
	renderCacheMu.Unlock()
	return result
}

// SidePanelWidth returns the width of the side panel column (including padding),
// or 0 if the terminal is not wide enough for a side panel.
func (b Banner) SidePanelWidth() int {
	if b.Size() == BannerSB {
		return sidePanelWidth
	}
	return 0
}

// SidePanelView returns the S-B art rendered for a side panel, vertically
// centered within the given height. Returns "" if terminal is too narrow.
func (b Banner) SidePanelView(height int) string {
	if b.Size() != BannerSB {
		return ""
	}

	key := bannerCacheKey{size: BannerSB, width: b.Width, height: height}
	renderCacheMu.Lock()
	if cached, ok := renderCache[key]; ok {
		renderCacheMu.Unlock()
		return cached
	}
	renderCacheMu.Unlock()

	styled := renderDopplerArt(artSB, sidePanelWidth, BannerSB)
	lines := strings.Split(styled, "\n")
	artHeight := len(lines)

	// Vertically center within the given height.
	if height > artHeight {
		topPad := (height - artHeight) / 2
		bottomPad := height - artHeight - topPad
		var padded []string
		for i := 0; i < topPad; i++ {
			padded = append(padded, "")
		}
		padded = append(padded, lines...)
		for i := 0; i < bottomPad; i++ {
			padded = append(padded, "")
		}
		styled = strings.Join(padded, "\n")
	}

	// Apply consistent panel width with padding.
	result := lipgloss.NewStyle().
		Width(sidePanelWidth).
		Render(styled)

	renderCacheMu.Lock()
	renderCache[key] = result
	renderCacheMu.Unlock()
	return result
}

// SplashView returns the XL art styled for the splash screen. Always uses the
// XL variant regardless of terminal width.
func (b Banner) SplashView() string {
	key := bannerCacheKey{size: BannerXL, width: b.Width}
	renderCacheMu.Lock()
	if cached, ok := renderCache[key]; ok {
		renderCacheMu.Unlock()
		return cached
	}
	renderCacheMu.Unlock()

	result := renderDopplerArt(artXL, b.Width, BannerXL)

	renderCacheMu.Lock()
	renderCache[key] = result
	renderCacheMu.Unlock()
	return result
}

// renderDopplerArt applies red/blue Doppler shift coloring to art lines and
// centers the result within the given width. The "Q U A S A R" text line
// gets logo-style coloring instead of Doppler shift.
func renderDopplerArt(art []string, width int, size BannerSize) string {
	var rendered []string
	for _, line := range art {
		if line == "" {
			rendered = append(rendered, "")
			continue
		}

		// Detect the QUASAR text line.
		trimmed := strings.TrimSpace(line)
		if trimmed == "Q    U    A    S    A    R" {
			styled := renderQuasarText(size)
			rendered = append(rendered, centerLine(styled, width))
			continue
		}

		styled := colorDopplerLine(line)
		rendered = append(rendered, centerLine(styled, width))
	}
	return strings.Join(rendered, "\n")
}

// renderQuasarText returns the "Q U A S A R" text with logo-style coloring.
// Only the XS-A Pill variant includes "····" decorators; other variants render
// the text without them, matching the original art designs.
func renderQuasarText(size BannerSize) string {
	core := styleLogoCore.Render("Q    U    A    S    A    R")
	if size == BannerXS {
		return styleLogoJet.Render("····") + " " + core + " " + styleLogoJet.Render("····")
	}
	return core
}

// centerLine pads a styled line to center it within the given width.
// Since the line contains ANSI codes, we compute the visual width from the
// raw rune count of the unstyled content.
func centerLine(styled string, width int) string {
	// lipgloss.Width handles ANSI-aware width measurement.
	vis := lipgloss.Width(styled)
	if vis >= width {
		return styled
	}
	pad := (width - vis) / 2
	return strings.Repeat(" ", pad) + styled
}

// colorDopplerLine applies Doppler shift coloring to a single art line.
// Characters on the left half get red/magenta tones; characters on the right
// half get blue/cyan tones. The '@' core character gets bright gold. Outer
// dots/particles fade to muted gray.
func colorDopplerLine(line string) string {
	runes := []rune(line)
	lineLen := len(runes)
	if lineLen == 0 {
		return ""
	}

	// Find the visual center of the line content (ignoring leading space).
	center := lineLen / 2

	var b strings.Builder
	b.Grow(lineLen * 4) // pre-allocate for ANSI overhead

	for i, r := range runes {
		ch := string(r)

		// Core characters get gold.
		if r == '@' {
			b.WriteString(styleCore.Render(ch))
			continue
		}

		// Whitespace passes through unstyled.
		if r == ' ' {
			b.WriteRune(r)
			continue
		}

		// Compute position ratio: 0.0 = far left, 1.0 = far right.
		ratio := float64(i) / float64(lineLen)

		// Outermost particles (dots, colons, mid-dots) fade to muted.
		if isFadeChar(r) && (ratio < 0.1 || ratio > 0.9) {
			b.WriteString(styleFade.Render(ch))
			continue
		}

		// Doppler shift: left = red, right = blue.
		if i < center {
			// Gradient: outer = danger red, inner = warm redshift.
			if ratio < 0.3 {
				b.WriteString(styleRedOuter.Render(ch))
			} else {
				b.WriteString(styleRedInner.Render(ch))
			}
		} else {
			// Gradient: inner = cool blueshift, outer = primary blue.
			if ratio > 0.7 {
				b.WriteString(styleBlueOuter.Render(ch))
			} else {
				b.WriteString(styleBlueInner.Render(ch))
			}
		}
	}

	return b.String()
}

// isFadeChar returns true if the rune is a dot/particle that should fade at edges.
func isFadeChar(r rune) bool {
	return r == '.' || r == '·' || r == ':'
}

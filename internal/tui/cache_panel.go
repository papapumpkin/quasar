package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/papapumpkin/quasar/internal/telemetry"
)

// CachePanel is a small read-only footer panel for the fleet detail view that
// surfaces prompt-cache effectiveness for the current phase. It summarizes the
// last N invocations as three token counts plus a derived hit rate — the
// killer metric: a hit rate below ~50% means the cache architecture is not
// paying off and there is a regression to find.
type CachePanel struct {
	Read    int // cache_read_input_tokens — tokens served from the cache
	Created int // cache_creation_input_tokens — tokens written to the cache
	Fresh   int // input_tokens — uncached tokens billed at full rate
	Cycles  int // number of cycles summarized (for the "(last N cycles)" suffix)
}

// cache panel styles.
var (
	styleCacheLabel = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
	styleCacheRead  = lipgloss.NewStyle().Foreground(colorSuccess)
	styleCacheDim   = lipgloss.NewStyle().Foreground(colorMutedLight)
	styleCacheArrow = lipgloss.NewStyle().Foreground(colorMuted)
)

// View renders the panel as a single line, e.g.:
//
//	Cache: read 18.2k / created 1.4k / fresh 2.1k  →  hit 84% (last 5 cycles)
//
// The hit percentage is colored green when caching is effective (≥50%) and red
// when it is not, so a regression stands out at a glance.
func (p CachePanel) View() string {
	ratio := telemetry.CacheHitRatio(p.Read, p.Fresh)
	hitPct := int(ratio*100 + 0.5)

	hitStyle := styleCacheRead
	if ratio < 0.5 {
		hitStyle = lipgloss.NewStyle().Foreground(colorDanger)
	}

	stats := fmt.Sprintf("read %s / created %s / fresh %s",
		styleCacheRead.Render(FormatTokens(p.Read)),
		styleCacheDim.Render(FormatTokens(p.Created)),
		styleCacheDim.Render(FormatTokens(p.Fresh)),
	)
	hit := hitStyle.Render(fmt.Sprintf("hit %d%%", hitPct))

	return fmt.Sprintf("%s %s  %s  %s %s",
		styleCacheLabel.Render("Cache:"),
		stats,
		styleCacheArrow.Render("→"),
		hit,
		styleCacheDim.Render(fmt.Sprintf("(last %s)", pluralCycles(p.Cycles))),
	)
}

// pluralCycles renders the cycle count with correct singular/plural wording.
func pluralCycles(n int) string {
	if n == 1 {
		return "1 cycle"
	}
	return fmt.Sprintf("%d cycles", n)
}

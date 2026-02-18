package tui

import (
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ─── Configuration ───────────────────────────────────────────────────────────

// SplashConfig controls the binary-star splash animation parameters.
type SplashConfig struct {
	Width     int
	Height    int
	OrbitRadX float64
	OrbitRadY float64
	SpikeLen  int
	FPS       int
	Spins     float64
	ShowTitle bool
	Loop      bool // if true, loops forever (spinner mode)
}

// DefaultSplashConfig returns a full-screen splash config: 62×19, 2 spins,
// settles to rest with an ease-out deceleration curve.
func DefaultSplashConfig() SplashConfig {
	return SplashConfig{
		Width:     62,
		Height:    19,
		OrbitRadX: 10,
		OrbitRadY: 3.5,
		SpikeLen:  6,
		FPS:       30,
		Spins:     2.0,
		ShowTitle: true,
		Loop:      false,
	}
}

// SpinnerConfig returns a compact, looping config for inline loading states.
func SpinnerConfig() SplashConfig {
	return SplashConfig{
		Width:     36,
		Height:    11,
		OrbitRadX: 6,
		OrbitRadY: 2.0,
		SpikeLen:  3,
		FPS:       30,
		Spins:     1.0, // ignored when Loop=true
		ShowTitle: true,
		Loop:      true,
	}
}

// ─── Color ramps for Doppler shift ──────────────────────────────────────────
// Each ramp goes from "near core" (index 0) to "far halo" (index 3).

type splashColorSet struct {
	core   lipgloss.Style
	spike  lipgloss.Style
	bright lipgloss.Style
	mid    lipgloss.Style
}

func makeSplashColorSet(coreHex, spikeHex, brightHex, midHex string) splashColorSet {
	return splashColorSet{
		core:   lipgloss.NewStyle().Foreground(lipgloss.Color(coreHex)).Bold(true),
		spike:  lipgloss.NewStyle().Foreground(lipgloss.Color(spikeHex)),
		bright: lipgloss.NewStyle().Foreground(lipgloss.Color(brightHex)),
		mid:    lipgloss.NewStyle().Foreground(lipgloss.Color(midHex)),
	}
}

// Precomputed color ramps indexed by Doppler shift bucket:
//
//	0 = max blueshift (approaching fast)
//	4 = neutral
//	8 = max redshift  (receding fast)
var splashDopplerRamps [9]splashColorSet

func init() {
	// Blue-shifted: cool whites → vivid blues.
	splashDopplerRamps[0] = makeSplashColorSet("#c0d8ff", "#70a8ff", "#4088e8", "#2060b0")
	splashDopplerRamps[1] = makeSplashColorSet("#d0e0ff", "#78b0f8", "#4890e0", "#2868b8")
	splashDopplerRamps[2] = makeSplashColorSet("#d8e4f8", "#80b8f0", "#5098d8", "#3070b0")

	// Neutral: warm gold / teal (the "at rest" palette).
	splashDopplerRamps[3] = makeSplashColorSet("#e8d878", "#6898c8", "#4888c0", "#3070a0")
	splashDopplerRamps[4] = makeSplashColorSet("#f0d870", "#6098d0", "#4080c0", "#305878")
	splashDopplerRamps[5] = makeSplashColorSet("#e8c868", "#6890b8", "#4878b0", "#306898")

	// Red-shifted: warm golds → deep reds.
	splashDopplerRamps[6] = makeSplashColorSet("#f0c050", "#c08848", "#a06838", "#704828")
	splashDopplerRamps[7] = makeSplashColorSet("#f0a040", "#d07040", "#b05030", "#803828")
	splashDopplerRamps[8] = makeSplashColorSet("#f08030", "#e06030", "#c04028", "#902820")
}

// ─── SplashModel ─────────────────────────────────────────────────────────────

// SplashModel implements tea.Model for a binary-star ASCII animation with
// Doppler color shifting. Use NewSplash to create one with a given config.
type SplashModel struct {
	cfg         SplashConfig
	frame       int
	totalFrames int
	done        bool

	// styleDim is the background/dim style (unaffected by Doppler).
	styleDim lipgloss.Style
}

// splashTickMsg drives the animation frame clock.
type splashTickMsg time.Time

// NewSplash creates a SplashModel configured by cfg.
func NewSplash(cfg SplashConfig) SplashModel {
	return SplashModel{
		cfg:         cfg,
		totalFrames: int(cfg.Spins * 120),
		styleDim:    lipgloss.NewStyle().Foreground(lipgloss.Color("#182038")),
	}
}

// Init starts the animation tick.
func (s SplashModel) Init() tea.Cmd { return s.tick() }

// Done reports whether the animation has finished. In splash mode it becomes
// true after the deceleration curve completes (plus a short hold) or when any
// key is pressed. In spinner/loop mode it never becomes true on its own.
func (s SplashModel) Done() bool { return s.done }

func (s SplashModel) tick() tea.Cmd {
	return tea.Tick(time.Second/time.Duration(s.cfg.FPS), func(t time.Time) tea.Msg {
		return splashTickMsg(t)
	})
}

// Update handles tick and key messages.
func (s SplashModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case tea.KeyMsg:
		if !s.cfg.Loop {
			s.done = true
			return s, nil
		}
	case splashTickMsg:
		if s.done {
			return s, nil
		}
		s.frame++
		if !s.cfg.Loop && s.frame >= s.totalFrames+10 {
			s.done = true
			return s, nil
		}
		return s, s.tick()
	}
	return s, nil
}

// View renders the current animation frame.
func (s SplashModel) View() string {
	angle := s.currentAngle()
	return s.renderFrame(angle)
}

func (s SplashModel) currentAngle() float64 {
	if s.cfg.Loop {
		// Continuous rotation — 6° per frame for brisk spin.
		return float64(s.frame) * 6.0 * math.Pi / 180.0
	}
	if s.done || s.frame >= s.totalFrames {
		return 0
	}
	progress := float64(s.frame) / float64(s.totalFrames)
	eased := 1.0 - math.Pow(1.0-progress, 2.5)
	return eased * s.cfg.Spins * 2 * math.Pi
}

// ─── Rendering ──────────────────────────────────────────────────────────────

// Color IDs stored per cell.
const (
	splashClrDim   = 0
	splashClrMid1  = 1
	splashClrBri1  = 2
	splashClrSpk1  = 3
	splashClrCore1 = 4
	splashClrMid2  = 5
	splashClrBri2  = 6
	splashClrSpk2  = 7
	splashClrCore2 = 8
)

func (s SplashModel) renderFrame(angle float64) string {
	w, h := s.cfg.Width, s.cfg.Height
	cx, cy := float64(w)/2, float64(h)/2-1

	// Star positions.
	s1x := cx + s.cfg.OrbitRadX*math.Cos(angle)
	s1y := cy + s.cfg.OrbitRadY*math.Sin(angle)
	s2x := cx + s.cfg.OrbitRadX*math.Cos(angle+math.Pi)
	s2y := cy + s.cfg.OrbitRadY*math.Sin(angle+math.Pi)

	// Doppler: velocity component along line of sight (sin of angle).
	// Star 1's radial velocity ∝ sin(angle), Star 2 is opposite.
	v1 := math.Sin(angle)
	v2 := math.Sin(angle + math.Pi)

	ramp1 := splashDopplerBucket(v1)
	ramp2 := splashDopplerBucket(v2)

	grid := make([][]rune, h)
	clr := make([][]int, h)

	for y := 0; y < h; y++ {
		grid[y] = make([]rune, w)
		clr[y] = make([]int, w)
		for x := 0; x < w; x++ {
			d1 := splashSDist(float64(x), float64(y), s1x, s1y)
			d2 := splashSDist(float64(x), float64(y), s2x, s2y)
			grid[y][x] = splashDensityChar(math.Min(d1, d2))

			// Assign color based on nearest star.
			if d1 < d2 {
				switch {
				case d1 < 5:
					clr[y][x] = splashClrBri1
				case d1 < 9:
					clr[y][x] = splashClrMid1
				default:
					clr[y][x] = splashClrDim
				}
			} else {
				switch {
				case d2 < 5:
					clr[y][x] = splashClrBri2
				case d2 < 9:
					clr[y][x] = splashClrMid2
				default:
					clr[y][x] = splashClrDim
				}
			}
		}
	}

	// Spikes and cores.
	splashStampSpikes(grid, clr, s1x, s1y, w, h, s.cfg.SpikeLen, splashClrSpk1, splashClrBri1)
	splashStampSpikes(grid, clr, s2x, s2y, w, h, s.cfg.SpikeLen, splashClrSpk2, splashClrBri2)
	splashStampCore(grid, clr, s1x, s1y, w, h, splashClrCore1)
	splashStampCore(grid, clr, s2x, s2y, w, h, splashClrCore2)

	// Resolve styles per cell.
	var sb strings.Builder
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ch := string(grid[y][x])
			switch clr[y][x] {
			case splashClrDim:
				sb.WriteString(s.styleDim.Render(ch))
			case splashClrMid1:
				sb.WriteString(ramp1.mid.Render(ch))
			case splashClrBri1:
				sb.WriteString(ramp1.bright.Render(ch))
			case splashClrSpk1:
				sb.WriteString(ramp1.spike.Render(ch))
			case splashClrCore1:
				sb.WriteString(ramp1.core.Render(ch))
			case splashClrMid2:
				sb.WriteString(ramp2.mid.Render(ch))
			case splashClrBri2:
				sb.WriteString(ramp2.bright.Render(ch))
			case splashClrSpk2:
				sb.WriteString(ramp2.spike.Render(ch))
			case splashClrCore2:
				sb.WriteString(ramp2.core.Render(ch))
			}
		}
		sb.WriteRune('\n')
	}

	if s.cfg.ShowTitle {
		title := "Q    U    A    S    A    R"
		pad := (w - len(title)) / 2
		if pad < 0 {
			pad = 0
		}
		titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#4a6888"))
		sb.WriteString(titleStyle.Render(strings.Repeat(" ", pad) + title))
		sb.WriteRune('\n')
	}

	return sb.String()
}

// splashDopplerBucket maps radial velocity [-1, 1] to a color ramp index [0, 8].
func splashDopplerBucket(v float64) splashColorSet {
	idx := int(math.Round((v + 1.0) * 4.0))
	if idx < 0 {
		idx = 0
	}
	if idx > 8 {
		idx = 8
	}
	return splashDopplerRamps[idx]
}

// ─── Shared helpers ─────────────────────────────────────────────────────────

var splashDensityRamp = []rune{' ', '.', '·', ':', ':', '*'}

func splashDensityChar(d float64) rune {
	switch {
	case d < 2:
		return splashDensityRamp[5]
	case d < 4:
		return splashDensityRamp[4]
	case d < 6:
		return splashDensityRamp[3]
	case d < 9:
		return splashDensityRamp[2]
	case d < 13:
		return splashDensityRamp[1]
	default:
		return splashDensityRamp[0]
	}
}

func splashSDist(x1, y1, x2, y2 float64) float64 {
	dx := x1 - x2
	dy := (y1 - y2) * 2.1
	return math.Sqrt(dx*dx + dy*dy)
}

func splashStampSpikes(grid [][]rune, clr [][]int, sx, sy float64, w, h, slen, spkClr, briClr int) {
	ix, iy := int(math.Round(sx)), int(math.Round(sy))
	dirs := [][3]int{
		{1, 0, '-'}, {-1, 0, '-'},
		{0, 1, '|'}, {0, -1, '|'},
		{1, -1, '/'}, {-1, 1, '/'},
		{1, 1, '\\'}, {-1, -1, '\\'},
	}
	for _, d := range dirs {
		for i := 1; i <= slen; i++ {
			x, y := ix+d[0]*i, iy+d[1]*i
			if x >= 0 && x < w && y >= 0 && y < h {
				switch {
				case i <= 2:
					grid[y][x] = rune(d[2])
					clr[y][x] = spkClr
				case i <= 4:
					grid[y][x] = rune(d[2])
					clr[y][x] = briClr
				default:
					grid[y][x] = '·'
					clr[y][x] = briClr
				}
			}
		}
	}
}

func splashStampCore(grid [][]rune, clr [][]int, sx, sy float64, w, h, coreClr int) {
	ix, iy := int(math.Round(sx)), int(math.Round(sy))
	if ix >= 0 && ix < w && iy >= 0 && iy < h {
		grid[iy][ix] = '@'
		clr[iy][ix] = coreClr
	}
	for _, d := range [][2]int{{-1, 0}, {1, 0}, {-2, 0}, {2, 0}} {
		x, y := ix+d[0], iy+d[1]
		if x >= 0 && x < w && y >= 0 && y < h && clr[y][x] != coreClr {
			grid[y][x] = '-'
			clr[y][x] = coreClr
		}
	}
}

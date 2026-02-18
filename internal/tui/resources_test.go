package tui

import (
	"strings"
	"testing"
)

func TestParsePSOutput(t *testing.T) {
	t.Parallel()

	t.Run("typical macOS output", func(t *testing.T) {
		t.Parallel()
		output := `  1234  102400   3.2
  1235   51200   1.5
  1236  204800  12.0
`
		snap := parsePSOutput(output)
		if snap.NumProcesses != 3 {
			t.Errorf("expected 3 processes, got %d", snap.NumProcesses)
		}
		// RSS: (102400 + 51200 + 204800) / 1024 = 350 MB
		wantMem := 350.0
		if snap.MemoryMB < wantMem-1 || snap.MemoryMB > wantMem+1 {
			t.Errorf("expected ~%.0f MB, got %.1f MB", wantMem, snap.MemoryMB)
		}
		// CPU: 3.2 + 1.5 + 12.0 = 16.7
		wantCPU := 16.7
		if snap.CPUPercent < wantCPU-0.1 || snap.CPUPercent > wantCPU+0.1 {
			t.Errorf("expected ~%.1f%% CPU, got %.1f%%", wantCPU, snap.CPUPercent)
		}
	})

	t.Run("empty output", func(t *testing.T) {
		t.Parallel()
		snap := parsePSOutput("")
		if snap.NumProcesses != 0 {
			t.Errorf("expected 0 processes for empty output, got %d", snap.NumProcesses)
		}
		if snap.MemoryMB != 0 {
			t.Errorf("expected 0 MB for empty output, got %.1f", snap.MemoryMB)
		}
	})

	t.Run("single process", func(t *testing.T) {
		t.Parallel()
		snap := parsePSOutput("  42  512000  25.5")
		if snap.NumProcesses != 1 {
			t.Errorf("expected 1 process, got %d", snap.NumProcesses)
		}
		wantMem := 500.0
		if snap.MemoryMB < wantMem-1 || snap.MemoryMB > wantMem+1 {
			t.Errorf("expected ~%.0f MB, got %.1f MB", wantMem, snap.MemoryMB)
		}
		if snap.CPUPercent != 25.5 {
			t.Errorf("expected 25.5%% CPU, got %.1f%%", snap.CPUPercent)
		}
	})

	t.Run("malformed lines skipped", func(t *testing.T) {
		t.Parallel()
		output := `  1234  102400   3.2
  bad line
  1236  204800  12.0
`
		snap := parsePSOutput(output)
		if snap.NumProcesses != 2 {
			t.Errorf("expected 2 valid processes, got %d", snap.NumProcesses)
		}
	})

	t.Run("whitespace-only output", func(t *testing.T) {
		t.Parallel()
		snap := parsePSOutput("   \n   \n")
		if snap.NumProcesses != 0 {
			t.Errorf("expected 0 processes for whitespace output, got %d", snap.NumProcesses)
		}
	})
}

func TestResourceLevels(t *testing.T) {
	t.Parallel()

	thresholds := DefaultResourceThresholds()

	t.Run("CPU normal", func(t *testing.T) {
		t.Parallel()
		snap := ResourceSnapshot{CPUPercent: 30}
		if snap.CPULevel(thresholds) != ResourceNormal {
			t.Error("expected ResourceNormal for 30% CPU")
		}
	})

	t.Run("CPU warning", func(t *testing.T) {
		t.Parallel()
		snap := ResourceSnapshot{CPUPercent: 60}
		if snap.CPULevel(thresholds) != ResourceWarning {
			t.Error("expected ResourceWarning for 60% CPU")
		}
	})

	t.Run("CPU danger", func(t *testing.T) {
		t.Parallel()
		snap := ResourceSnapshot{CPUPercent: 90}
		if snap.CPULevel(thresholds) != ResourceDanger {
			t.Error("expected ResourceDanger for 90% CPU")
		}
	})

	t.Run("memory normal", func(t *testing.T) {
		t.Parallel()
		snap := ResourceSnapshot{MemoryMB: 500}
		if snap.MemoryLevel(thresholds) != ResourceNormal {
			t.Error("expected ResourceNormal for 500 MB")
		}
	})

	t.Run("memory warning", func(t *testing.T) {
		t.Parallel()
		snap := ResourceSnapshot{MemoryMB: 1500}
		if snap.MemoryLevel(thresholds) != ResourceWarning {
			t.Error("expected ResourceWarning for 1500 MB")
		}
	})

	t.Run("memory danger", func(t *testing.T) {
		t.Parallel()
		snap := ResourceSnapshot{MemoryMB: 3000}
		if snap.MemoryLevel(thresholds) != ResourceDanger {
			t.Error("expected ResourceDanger for 3000 MB")
		}
	})

	t.Run("worst level uses highest", func(t *testing.T) {
		t.Parallel()
		snap := ResourceSnapshot{CPUPercent: 30, MemoryMB: 3000}
		if snap.WorstLevel(thresholds) != ResourceDanger {
			t.Error("expected ResourceDanger when memory is danger")
		}
	})

	t.Run("worst level CPU dominates", func(t *testing.T) {
		t.Parallel()
		snap := ResourceSnapshot{CPUPercent: 90, MemoryMB: 100}
		if snap.WorstLevel(thresholds) != ResourceDanger {
			t.Error("expected ResourceDanger when CPU is danger")
		}
	})

	t.Run("boundary at exact threshold", func(t *testing.T) {
		t.Parallel()
		snap := ResourceSnapshot{CPUPercent: 50}
		if snap.CPULevel(thresholds) != ResourceWarning {
			t.Error("expected ResourceWarning at exactly 50% CPU")
		}
	})
}

func TestFormatResourceIndicator(t *testing.T) {
	t.Parallel()

	t.Run("typical snapshot", func(t *testing.T) {
		t.Parallel()
		snap := ResourceSnapshot{
			NumProcesses: 2,
			MemoryMB:     48,
			CPUPercent:   3.2,
		}
		result := FormatResourceIndicator(snap)
		if !strings.Contains(result, "◈2") {
			t.Errorf("expected process count ◈2, got: %s", result)
		}
		if !strings.Contains(result, "48MB") {
			t.Errorf("expected 48MB, got: %s", result)
		}
		if !strings.Contains(result, "3.2%") {
			t.Errorf("expected 3.2%%, got: %s", result)
		}
	})

	t.Run("large memory uses GB", func(t *testing.T) {
		t.Parallel()
		snap := ResourceSnapshot{
			NumProcesses: 4,
			MemoryMB:     2048,
			CPUPercent:   50.0,
		}
		result := FormatResourceIndicator(snap)
		if !strings.Contains(result, "2.0GB") {
			t.Errorf("expected 2.0GB for 2048MB, got: %s", result)
		}
	})

	t.Run("empty snapshot returns empty string", func(t *testing.T) {
		t.Parallel()
		snap := ResourceSnapshot{}
		result := FormatResourceIndicator(snap)
		if result != "" {
			t.Errorf("expected empty string for zero snapshot, got: %s", result)
		}
	})
}

func TestFormatQuasarCount(t *testing.T) {
	t.Parallel()

	t.Run("single instance returns empty", func(t *testing.T) {
		t.Parallel()
		if result := FormatQuasarCount(1); result != "" {
			t.Errorf("expected empty string for count=1, got: %s", result)
		}
	})

	t.Run("zero returns empty", func(t *testing.T) {
		t.Parallel()
		if result := FormatQuasarCount(0); result != "" {
			t.Errorf("expected empty string for count=0, got: %s", result)
		}
	})

	t.Run("multiple instances shown", func(t *testing.T) {
		t.Parallel()
		result := FormatQuasarCount(3)
		if !strings.Contains(result, "3") {
			t.Errorf("expected count 3, got: %s", result)
		}
		if !strings.Contains(result, "quasars") {
			t.Errorf("expected 'quasars' label, got: %s", result)
		}
	})
}

func TestDefaultResourceThresholds(t *testing.T) {
	t.Parallel()

	thresholds := DefaultResourceThresholds()
	if thresholds.CPUWarningPercent != 50 {
		t.Errorf("expected CPUWarningPercent=50, got %.0f", thresholds.CPUWarningPercent)
	}
	if thresholds.CPUDangerPercent != 80 {
		t.Errorf("expected CPUDangerPercent=80, got %.0f", thresholds.CPUDangerPercent)
	}
	if thresholds.MemoryWarningMB != 1024 {
		t.Errorf("expected MemoryWarningMB=1024, got %.0f", thresholds.MemoryWarningMB)
	}
	if thresholds.MemoryDangerMB != 2048 {
		t.Errorf("expected MemoryDangerMB=2048, got %.0f", thresholds.MemoryDangerMB)
	}
}

func TestResourceLevelStyle(t *testing.T) {
	t.Parallel()

	t.Run("normal is not bold", func(t *testing.T) {
		t.Parallel()
		style := resourceLevelStyle(ResourceNormal)
		if style.GetBold() {
			t.Error("normal level should not be bold")
		}
	})

	t.Run("danger uses uniform style", func(t *testing.T) {
		t.Parallel()
		style := resourceLevelStyle(ResourceDanger)
		if style.GetBold() {
			t.Error("danger level should not be bold in uniform bar")
		}
	})
}

func TestStatusBarWithResources(t *testing.T) {
	t.Parallel()

	t.Run("resource indicator shown in status bar", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			Width:      120,
			Thresholds: DefaultResourceThresholds(),
			Resources: ResourceSnapshot{
				NumProcesses: 3,
				MemoryMB:     256,
				CPUPercent:   15.5,
			},
		}
		view := sb.View()
		if !strings.Contains(view, "◈3") {
			t.Errorf("expected process count ◈3 in status bar, got: %s", view)
		}
		if !strings.Contains(view, "256MB") {
			t.Errorf("expected 256MB in status bar, got: %s", view)
		}
		if !strings.Contains(view, "15.5%%") {
			// lipgloss rendering in tests may strip ANSI — check raw content.
			if !strings.Contains(view, "15.5") {
				t.Errorf("expected 15.5%% CPU in status bar, got: %s", view)
			}
		}
	})

	t.Run("no resources shown when snapshot is empty", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			Width:      120,
			Thresholds: DefaultResourceThresholds(),
		}
		view := sb.View()
		if strings.Contains(view, "◈") {
			t.Errorf("expected no resource indicator for empty snapshot, got: %s", view)
		}
	})

	t.Run("multi-quasar indicator shown", func(t *testing.T) {
		t.Parallel()
		sb := StatusBar{
			Width:      120,
			Thresholds: DefaultResourceThresholds(),
			Resources: ResourceSnapshot{
				NumProcesses: 2,
				MemoryMB:     128,
				CPUPercent:   5.0,
				QuasarCount:  3,
			},
		}
		view := sb.View()
		if !strings.Contains(view, "quasars") {
			t.Errorf("expected multi-quasar indicator for count=3, got: %s", view)
		}
	})
}

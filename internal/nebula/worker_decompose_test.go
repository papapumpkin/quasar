package nebula

import (
	"testing"
)

func TestShouldDecompose(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		phase          *PhaseSpec
		invokerSet     bool
		manifestAuto   bool
		want           bool
	}{
		{
			name:         "EnabledViaManifest",
			phase:        &PhaseSpec{ID: "test"},
			invokerSet:   true,
			manifestAuto: true,
			want:         true,
		},
		{
			name:         "DisabledByDefault",
			phase:        &PhaseSpec{ID: "test"},
			invokerSet:   true,
			manifestAuto: false,
			want:         false,
		},
		{
			name:         "DecomposedPhaseBlocked",
			phase:        &PhaseSpec{ID: "test", Decomposed: true},
			invokerSet:   true,
			manifestAuto: true,
			want:         false,
		},
		{
			name:         "NoInvoker",
			phase:        &PhaseSpec{ID: "test"},
			invokerSet:   false,
			manifestAuto: true,
			want:         false,
		},
		{
			name:         "PerPhaseOverrideTrue",
			phase:        &PhaseSpec{ID: "test", AutoDecompose: boolPtr(true)},
			invokerSet:   true,
			manifestAuto: false, // manifest says no, but phase says yes
			want:         true,
		},
		{
			name:         "PerPhaseOverrideFalse",
			phase:        &PhaseSpec{ID: "test", AutoDecompose: boolPtr(false)},
			invokerSet:   true,
			manifestAuto: true, // manifest says yes, but phase says no
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wg := &WorkerGroup{
				Nebula: &Nebula{
					Manifest: Manifest{
						Execution: Execution{AutoDecompose: tt.manifestAuto},
					},
				},
			}
			if tt.invokerSet {
				wg.Invoker = &mockInvoker{}
			}
			got := wg.shouldDecompose(tt.phase)
			if got != tt.want {
				t.Errorf("shouldDecompose() = %v, want %v", got, tt.want)
			}
		})
	}
}

func boolPtr(b bool) *bool {
	return &b
}

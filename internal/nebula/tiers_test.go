package nebula

import (
	"strings"
	"testing"
)

func TestSelectTier(t *testing.T) {
	t.Parallel()

	tiers := []ModelTier{
		{Name: "fast", Model: "claude-haiku", MaxScore: 0.35},
		{Name: "balanced", Model: "claude-sonnet", MaxScore: 0.70},
		{Name: "heavy", Model: "claude-opus", MaxScore: 1.00},
	}

	tests := []struct {
		name     string
		score    float64
		wantTier string
	}{
		{name: "zero score", score: 0.0, wantTier: "fast"},
		{name: "below fast boundary", score: 0.20, wantTier: "fast"},
		{name: "exact fast boundary", score: 0.35, wantTier: "fast"},
		{name: "just above fast", score: 0.36, wantTier: "balanced"},
		{name: "mid balanced", score: 0.50, wantTier: "balanced"},
		{name: "exact balanced boundary", score: 0.70, wantTier: "balanced"},
		{name: "just above balanced", score: 0.71, wantTier: "heavy"},
		{name: "exact heavy boundary", score: 1.00, wantTier: "heavy"},
		{name: "above all thresholds fallback", score: 1.50, wantTier: "heavy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SelectTier(tt.score, tiers)
			if got.Name != tt.wantTier {
				t.Errorf("SelectTier(%f) = %q, want %q", tt.score, got.Name, tt.wantTier)
			}
		})
	}
}

func TestSelectTierWithDefaults(t *testing.T) {
	t.Parallel()

	// Verify DefaultTiers works with SelectTier.
	got := SelectTier(0.10, DefaultTiers)
	if got.Name != "fast" {
		t.Errorf("SelectTier(0.10, DefaultTiers) = %q, want %q", got.Name, "fast")
	}

	got = SelectTier(0.50, DefaultTiers)
	if got.Name != "balanced" {
		t.Errorf("SelectTier(0.50, DefaultTiers) = %q, want %q", got.Name, "balanced")
	}

	got = SelectTier(0.90, DefaultTiers)
	if got.Name != "heavy" {
		t.Errorf("SelectTier(0.90, DefaultTiers) = %q, want %q", got.Name, "heavy")
	}
}

func TestValidateRouting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cfg        TierConfig
		wantErrs   int
		wantSubstr string // substring expected in at least one error message
	}{
		{
			name: "disabled config returns no errors",
			cfg: TierConfig{
				Enabled: false,
				Tiers: []ModelTier{
					{Name: "bad", Model: "", MaxScore: 0.5},
				},
			},
			wantErrs: 0,
		},
		{
			name: "enabled with no tiers is valid",
			cfg: TierConfig{
				Enabled: true,
				Tiers:   nil,
			},
			wantErrs: 0,
		},
		{
			name: "valid three-tier config",
			cfg: TierConfig{
				Enabled: true,
				Tiers: []ModelTier{
					{Name: "fast", Model: "claude-haiku", MaxScore: 0.35},
					{Name: "balanced", Model: "claude-sonnet", MaxScore: 0.70},
					{Name: "heavy", Model: "claude-opus", MaxScore: 1.00},
				},
			},
			wantErrs: 0,
		},
		{
			name: "empty model field",
			cfg: TierConfig{
				Enabled: true,
				Tiers: []ModelTier{
					{Name: "fast", Model: "", MaxScore: 0.35},
					{Name: "heavy", Model: "claude-opus", MaxScore: 1.00},
				},
			},
			wantErrs:   1,
			wantSubstr: "empty model",
		},
		{
			name: "duplicate tier names",
			cfg: TierConfig{
				Enabled: true,
				Tiers: []ModelTier{
					{Name: "fast", Model: "claude-haiku", MaxScore: 0.35},
					{Name: "fast", Model: "claude-sonnet", MaxScore: 1.00},
				},
			},
			wantErrs:   1,
			wantSubstr: "duplicate tier name",
		},
		{
			name: "unsorted tiers",
			cfg: TierConfig{
				Enabled: true,
				Tiers: []ModelTier{
					{Name: "heavy", Model: "claude-opus", MaxScore: 1.00},
					{Name: "fast", Model: "claude-haiku", MaxScore: 0.35},
				},
			},
			wantErrs:   2, // unsorted + last tier < 1.0
			wantSubstr: "not sorted",
		},
		{
			name: "last tier below 1.0",
			cfg: TierConfig{
				Enabled: true,
				Tiers: []ModelTier{
					{Name: "fast", Model: "claude-haiku", MaxScore: 0.35},
					{Name: "balanced", Model: "claude-sonnet", MaxScore: 0.70},
				},
			},
			wantErrs:   1,
			wantSubstr: "must be >= 1.0",
		},
		{
			name: "multiple errors",
			cfg: TierConfig{
				Enabled: true,
				Tiers: []ModelTier{
					{Name: "a", Model: "m1", MaxScore: 0.80},
					{Name: "a", Model: "", MaxScore: 0.50}, // unsorted, duplicate name, empty model, last < 1.0
				},
			},
			wantErrs: 4, // empty model + duplicate name + unsorted + last tier < 1.0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			errs := ValidateRouting(tt.cfg)
			if len(errs) != tt.wantErrs {
				t.Errorf("ValidateRouting() returned %d errors, want %d", len(errs), tt.wantErrs)
				for _, e := range errs {
					t.Logf("  error: %s", e.Err)
				}
				return
			}
			if tt.wantSubstr != "" && len(errs) > 0 {
				found := false
				for _, e := range errs {
					if strings.Contains(e.Err.Error(), tt.wantSubstr) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("no error contains %q", tt.wantSubstr)
					for _, e := range errs {
						t.Logf("  error: %s", e.Err)
					}
				}
			}
			// All routing errors should have the correct category.
			for _, e := range errs {
				if e.Category != ValCatInvalidRouting {
					t.Errorf("error category = %q, want %q", e.Category, ValCatInvalidRouting)
				}
			}
		})
	}
}

func TestDefaultTiers(t *testing.T) {
	t.Parallel()

	if len(DefaultTiers) != 3 {
		t.Fatalf("DefaultTiers has %d entries, want 3", len(DefaultTiers))
	}

	// Verify they cover [0.0, 1.0].
	if DefaultTiers[0].MaxScore <= 0 {
		t.Errorf("first default tier MaxScore = %f, want > 0", DefaultTiers[0].MaxScore)
	}
	if DefaultTiers[len(DefaultTiers)-1].MaxScore < 1.0 {
		t.Errorf("last default tier MaxScore = %f, want >= 1.0", DefaultTiers[len(DefaultTiers)-1].MaxScore)
	}

	// Verify sort order.
	for i := 1; i < len(DefaultTiers); i++ {
		if DefaultTiers[i].MaxScore <= DefaultTiers[i-1].MaxScore {
			t.Errorf("DefaultTiers not sorted: [%d].MaxScore (%f) <= [%d].MaxScore (%f)",
				i, DefaultTiers[i].MaxScore, i-1, DefaultTiers[i-1].MaxScore)
		}
	}

	// Verify all models are non-empty.
	for i, tier := range DefaultTiers {
		if tier.Model == "" {
			t.Errorf("DefaultTiers[%d].Model is empty", i)
		}
		if tier.Name == "" {
			t.Errorf("DefaultTiers[%d].Name is empty", i)
		}
	}
}

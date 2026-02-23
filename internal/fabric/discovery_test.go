package fabric

import (
	"context"
	"testing"
)

func TestIsHail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind string
		want bool
	}{
		{DiscoveryEntanglementDispute, true},
		{DiscoveryMissingDependency, true},
		{DiscoveryFileConflict, true},
		{DiscoveryRequirementsAmbiguity, true},
		{DiscoveryBudgetAlert, false},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			t.Parallel()
			d := Discovery{Kind: tt.kind}
			if got := d.IsHail(); got != tt.want {
				t.Errorf("Discovery{Kind: %q}.IsHail() = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
}

func TestValidateDiscoveryKind(t *testing.T) {
	t.Parallel()

	t.Run("valid kinds", func(t *testing.T) {
		t.Parallel()
		valid := []string{
			DiscoveryEntanglementDispute,
			DiscoveryMissingDependency,
			DiscoveryFileConflict,
			DiscoveryRequirementsAmbiguity,
			DiscoveryBudgetAlert,
		}
		for _, kind := range valid {
			if err := ValidateDiscoveryKind(kind); err != nil {
				t.Errorf("ValidateDiscoveryKind(%q) = %v, want nil", kind, err)
			}
		}
	})

	t.Run("invalid kind", func(t *testing.T) {
		t.Parallel()
		if err := ValidateDiscoveryKind("invalid_kind"); err == nil {
			t.Error("ValidateDiscoveryKind(\"invalid_kind\") = nil, want error")
		}
	})

	t.Run("empty kind", func(t *testing.T) {
		t.Parallel()
		if err := ValidateDiscoveryKind(""); err == nil {
			t.Error("ValidateDiscoveryKind(\"\") = nil, want error")
		}
	})
}

func TestPendingHails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := testFabric(t)

	// Post a mix of discovery kinds.
	discoveries := []Discovery{
		{SourceTask: "p1", Kind: DiscoveryFileConflict, Detail: "conflict"},
		{SourceTask: "p2", Kind: DiscoveryBudgetAlert, Detail: "budget warning"},
		{SourceTask: "p1", Kind: DiscoveryMissingDependency, Detail: "missing dep"},
	}
	for _, d := range discoveries {
		if _, err := b.PostDiscovery(ctx, d); err != nil {
			t.Fatalf("PostDiscovery(%q): %v", d.Kind, err)
		}
	}

	hails, err := PendingHails(ctx, b)
	if err != nil {
		t.Fatalf("PendingHails: %v", err)
	}

	// budget_alert should be excluded, leaving 2 hails.
	if len(hails) != 2 {
		t.Fatalf("len(PendingHails) = %d, want 2", len(hails))
	}
	for _, h := range hails {
		if h.Kind == DiscoveryBudgetAlert {
			t.Errorf("PendingHails included budget_alert discovery")
		}
	}
}

func TestPendingHails_ExcludesResolved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := testFabric(t)

	// Post and immediately resolve a hail-worthy discovery.
	id, err := b.PostDiscovery(ctx, Discovery{SourceTask: "p1", Kind: DiscoveryFileConflict, Detail: "conflict"})
	if err != nil {
		t.Fatalf("PostDiscovery: %v", err)
	}
	if err := b.ResolveDiscovery(ctx, id); err != nil {
		t.Fatalf("ResolveDiscovery: %v", err)
	}

	hails, err := PendingHails(ctx, b)
	if err != nil {
		t.Fatalf("PendingHails: %v", err)
	}
	if len(hails) != 0 {
		t.Errorf("len(PendingHails) = %d, want 0 (resolved discovery should be excluded)", len(hails))
	}
}

func TestPostDiscovery_ReturnsID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := testFabric(t)

	id1, err := b.PostDiscovery(ctx, Discovery{SourceTask: "p1", Kind: DiscoveryFileConflict, Detail: "first"})
	if err != nil {
		t.Fatalf("PostDiscovery: %v", err)
	}
	if id1 == 0 {
		t.Error("expected non-zero ID for first discovery")
	}

	id2, err := b.PostDiscovery(ctx, Discovery{SourceTask: "p1", Kind: DiscoveryBudgetAlert, Detail: "second"})
	if err != nil {
		t.Fatalf("PostDiscovery: %v", err)
	}
	if id2 <= id1 {
		t.Errorf("second ID (%d) should be greater than first (%d)", id2, id1)
	}
}

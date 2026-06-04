package fabric

import (
	"context"
	"testing"
)

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

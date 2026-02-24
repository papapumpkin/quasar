package fabric

import (
	"context"
	"fmt"
)

// ValidDiscoveryKinds is the set of accepted discovery kinds.
var ValidDiscoveryKinds = map[string]bool{
	DiscoveryEntanglementDispute:   true,
	DiscoveryMissingDependency:     true,
	DiscoveryFileConflict:          true,
	DiscoveryRequirementsAmbiguity: true,
	DiscoveryBudgetAlert:           true,
}

// ValidateDiscoveryKind returns an error if kind is not a recognized discovery kind.
func ValidateDiscoveryKind(kind string) error {
	if !ValidDiscoveryKinds[kind] {
		return fmt.Errorf("invalid discovery kind %q: must be one of entanglement_dispute, missing_dependency, file_conflict, requirements_ambiguity, budget_alert", kind)
	}
	return nil
}

// IsHail returns true if this discovery should surface as a human interrupt.
// Budget alerts are informational and do not require human attention.
func (d Discovery) IsHail() bool {
	return d.Kind != DiscoveryBudgetAlert
}

// PendingHails returns unresolved discoveries that require human attention.
func PendingHails(ctx context.Context, f Fabric) ([]Discovery, error) {
	all, err := f.UnresolvedDiscoveries(ctx)
	if err != nil {
		return nil, err
	}
	var hails []Discovery
	for _, d := range all {
		if d.IsHail() {
			hails = append(hails, d)
		}
	}
	return hails, nil
}

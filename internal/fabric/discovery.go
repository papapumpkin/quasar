package fabric

import (
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

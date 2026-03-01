package loop

import "fmt"

// LifecycleSummary holds counts of finding status transitions for a single cycle.
type LifecycleSummary struct {
	Fixed        int
	StillPresent int
	Regressed    int
}

// String returns a compact summary like "2 fixed, 1 still present, 0 regressed".
func (s LifecycleSummary) String() string {
	return fmt.Sprintf("%d fixed, %d still present, %d regressed",
		s.Fixed, s.StillPresent, s.Regressed)
}

// HasUnresolved returns true if any findings are still present or regressed.
func (s LifecycleSummary) HasUnresolved() bool {
	return s.StillPresent > 0 || s.Regressed > 0
}

// ApplyVerifications matches verification results to accumulated findings by ID
// and updates their Status field. Returns counts of each status transition for
// UI reporting. Findings with no matching verification retain their current status.
func ApplyVerifications(allFindings []ReviewFinding, verifications []FindingVerification) LifecycleSummary {
	byID := make(map[string]*ReviewFinding, len(allFindings))
	for i := range allFindings {
		byID[allFindings[i].ID] = &allFindings[i]
	}

	var summary LifecycleSummary
	for _, v := range verifications {
		f, ok := byID[v.FindingID]
		if !ok {
			continue
		}
		f.Status = v.Status
		switch v.Status {
		case FindingStatusFixed:
			summary.Fixed++
		case FindingStatusStillPresent:
			summary.StillPresent++
		case FindingStatusRegressed:
			summary.Regressed++
		}
	}
	return summary
}

package loop

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// FindingID computes a deterministic identifier for a finding based on its
// severity and description. The ID is a short hex prefix of a SHA-256 hash,
// stable across cycles so the same logical finding can be tracked.
func FindingID(severity, description string) string {
	h := sha256.New()
	h.Write([]byte(severity))
	h.Write([]byte(":"))
	h.Write([]byte(strings.TrimSpace(description)))
	return fmt.Sprintf("f-%x", h.Sum(nil)[:6])
}

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

// SerializeFindings renders a slice of ReviewFinding into a compact text block
// suitable for injection into the reviewer prompt. Each finding is formatted as
// a numbered entry with its ID, severity, cycle of origin, current status, and
// a truncated description. An empty slice produces an empty string.
func SerializeFindings(findings []ReviewFinding, maxDescLen int) string {
	if len(findings) == 0 {
		return ""
	}
	var b strings.Builder
	for i, f := range findings {
		desc := truncate(f.Description, maxDescLen)
		fmt.Fprintf(&b, "%d. [%s] id=%s cycle=%d status=%s\n   %s\n",
			i+1, f.Severity, f.ID, f.Cycle, f.Status, desc)
	}
	return b.String()
}

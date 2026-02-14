package loop

import (
	"fmt"
	"strings"
)

// ReviewReport captures structured metadata from the reviewer's REPORT: block.
type ReviewReport struct {
	Satisfaction     string `toml:"satisfaction"`       // high, medium, low
	Risk             string `toml:"risk"`               // high, medium, low
	NeedsHumanReview bool   `toml:"needs_human_review"`
	Summary          string `toml:"summary"`
}

// ParseReviewReport extracts a REPORT: block from reviewer output.
// Returns nil if no report block is found.
func ParseReviewReport(output string) *ReviewReport {
	lines := strings.Split(output, "\n")
	i := 0
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) == "REPORT:" {
			i++
			report := &ReviewReport{}
			found := false
			for i < len(lines) {
				line := strings.TrimSpace(lines[i])
				if line == "" {
					i++
					continue
				}
				if strings.HasPrefix(line, "SATISFACTION:") {
					report.Satisfaction = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "SATISFACTION:")))
					found = true
				} else if strings.HasPrefix(line, "RISK:") {
					report.Risk = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "RISK:")))
					found = true
				} else if strings.HasPrefix(line, "NEEDS_HUMAN_REVIEW:") {
					val := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "NEEDS_HUMAN_REVIEW:")))
					report.NeedsHumanReview = val == "yes" || val == "true"
					found = true
				} else if strings.HasPrefix(line, "SUMMARY:") {
					report.Summary = strings.TrimSpace(strings.TrimPrefix(line, "SUMMARY:"))
					found = true
				} else {
					break
				}
				i++
			}
			if found {
				return report
			}
			continue
		}
		i++
	}
	return nil
}

// FormatReportComment formats a ReviewReport as a beads comment string.
func FormatReportComment(r *ReviewReport) string {
	humanReview := "no"
	if r.NeedsHumanReview {
		humanReview = "yes"
	}
	return fmt.Sprintf("[reviewer report]\nSatisfaction: %s\nRisk: %s\nNeeds human review: %s\nSummary: %s",
		r.Satisfaction, r.Risk, humanReview, r.Summary)
}

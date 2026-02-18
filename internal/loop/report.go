package loop

import (
	"fmt"
	"strings"

	"github.com/papapumpkin/quasar/internal/agent"
)

// ParseReviewReport extracts a REPORT: block from reviewer output.
// Returns nil if no report block is found.
func ParseReviewReport(output string) *agent.ReviewReport {
	lines := strings.Split(output, "\n")
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "REPORT:" {
			continue
		}
		report, ok := parseReportBlock(lines[i+1:])
		if ok {
			return report
		}
	}
	return nil
}

// parseReportBlock parses structured fields from lines following a REPORT: header.
// Returns the report and true if at least one field was found.
func parseReportBlock(lines []string) (*agent.ReviewReport, bool) {
	report := &agent.ReviewReport{}
	found := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "SATISFACTION:"):
			report.Satisfaction = parseField(line, "SATISFACTION:")
			found = true
		case strings.HasPrefix(line, "RISK:"):
			report.Risk = parseField(line, "RISK:")
			found = true
		case strings.HasPrefix(line, "NEEDS_HUMAN_REVIEW:"):
			val := parseField(line, "NEEDS_HUMAN_REVIEW:")
			report.NeedsHumanReview = val == "yes" || val == "true"
			found = true
		case strings.HasPrefix(line, "SUMMARY:"):
			report.Summary = strings.TrimSpace(strings.TrimPrefix(line, "SUMMARY:"))
			found = true
		default:
			return report, found
		}
	}
	return report, found
}

// parseField extracts and normalizes a field value by trimming the prefix,
// whitespace, and converting to lowercase.
func parseField(line, prefix string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, prefix)))
}

// FormatReportComment formats a ReviewReport as a beads comment string.
func FormatReportComment(r *agent.ReviewReport) string {
	humanReview := "no"
	if r.NeedsHumanReview {
		humanReview = "yes"
	}
	return fmt.Sprintf("[reviewer report]\nSatisfaction: %s\nRisk: %s\nNeeds human review: %s\nSummary: %s",
		r.Satisfaction, r.Risk, humanReview, r.Summary)
}

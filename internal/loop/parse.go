package loop

import "strings"

// ParseReviewFindings scans reviewer output for structured ISSUE: blocks.
func ParseReviewFindings(output string) []ReviewFinding {
	var findings []ReviewFinding
	lines := strings.Split(output, "\n")
	for i := 0; i < len(lines); {
		if strings.TrimSpace(lines[i]) == "ISSUE:" {
			f, next := parseIssueBlock(lines, i+1)
			if f.Description != "" {
				if f.Severity == "" {
					f.Severity = "major"
				}
				f.ID = FindingID(f.Severity, f.Description)
				f.Status = FindingStatusFound
				findings = append(findings, f)
			}
			i = next
			continue
		}
		i++
	}
	return findings
}

// parseIssueBlock parses a single ISSUE: block starting at index start.
// It returns the parsed finding and the index to resume scanning from.
func parseIssueBlock(lines []string, start int) (ReviewFinding, int) {
	f := ReviewFinding{}
	i := start
	for i < len(lines) {
		inner := strings.TrimSpace(lines[i])
		if inner == "" || inner == "ISSUE:" {
			break
		}
		switch {
		case strings.HasPrefix(inner, "SEVERITY:"):
			f.Severity = strings.TrimSpace(strings.TrimPrefix(inner, "SEVERITY:"))
			i++
		case strings.HasPrefix(inner, "DESCRIPTION:"):
			f.Description = strings.TrimSpace(strings.TrimPrefix(inner, "DESCRIPTION:"))
			i++
			i = collectContinuationLines(&f, lines, i)
		default:
			i++
		}
	}
	return f, i
}

// collectContinuationLines appends subsequent non-field lines to the finding's
// description. It returns the index of the first non-continuation line.
func collectContinuationLines(f *ReviewFinding, lines []string, start int) int {
	i := start
	for i < len(lines) {
		cont := strings.TrimSpace(lines[i])
		if cont == "" || cont == "ISSUE:" || strings.HasPrefix(cont, "SEVERITY:") || strings.HasPrefix(cont, "APPROVED:") {
			break
		}
		f.Description += " " + cont
		i++
	}
	return i
}

func isApproved(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "APPROVED:") {
			return true
		}
	}
	return false
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... [truncated]"
}

// firstLine extracts the first line of text and truncates to maxLen characters.
func firstLine(s string, maxLen int) string {
	line, _, _ := strings.Cut(s, "\n")
	line = strings.TrimSpace(line)
	if len(line) > maxLen {
		return line[:maxLen]
	}
	return line
}

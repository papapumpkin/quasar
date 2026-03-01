+++
id = "verification-parsing"
title = "Parse VERIFICATION: blocks from reviewer output into structured finding updates"
type = "feature"
priority = 1
depends_on = ["prompt-injection"]
labels = ["quasar", "reviewer", "quality"]
+++

## Problem

After phase 02, the reviewer is instructed to emit `VERIFICATION:` blocks reporting the status of each prior finding. But the loop has no parser for these blocks. The verification data is lost — `ParseReviewFindings` only extracts `ISSUE:` blocks and ignores everything else.

## Solution

Add a `ParseVerifications` function that extracts `VERIFICATION:` blocks from reviewer output and returns a structured list of finding status updates. Wire it into `runReviewerPhase` so verifications are captured alongside new findings.

### 1. New type `FindingVerification`

In `internal/loop/state.go`:

```go
// FindingVerification represents the reviewer's assessment of a prior finding.
type FindingVerification struct {
	FindingID string        // matches ReviewFinding.ID
	Status    FindingStatus // fixed, still_present, regressed
	Comment   string        // reviewer's explanation
}
```

### 2. Parser in `internal/loop/parse.go`

```go
// ParseVerifications scans reviewer output for structured VERIFICATION: blocks.
// Each block is expected to contain FINDING_ID:, STATUS:, and optionally COMMENT: fields.
// Unknown statuses are treated as still_present to be conservative.
func ParseVerifications(output string) []FindingVerification {
	var verifications []FindingVerification
	lines := strings.Split(output, "\n")
	for i := 0; i < len(lines); {
		if strings.TrimSpace(lines[i]) == "VERIFICATION:" {
			v, next := parseVerificationBlock(lines, i+1)
			if v.FindingID != "" {
				verifications = append(verifications, v)
			}
			i = next
			continue
		}
		i++
	}
	return verifications
}

// parseVerificationBlock parses a single VERIFICATION: block starting at index start.
func parseVerificationBlock(lines []string, start int) (FindingVerification, int) {
	v := FindingVerification{}
	i := start
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if line == "" || line == "VERIFICATION:" || line == "ISSUE:" || line == "REPORT:" {
			break
		}
		switch {
		case strings.HasPrefix(line, "FINDING_ID:"):
			v.FindingID = strings.TrimSpace(strings.TrimPrefix(line, "FINDING_ID:"))
		case strings.HasPrefix(line, "STATUS:"):
			v.Status = parseVerificationStatus(strings.TrimSpace(strings.TrimPrefix(line, "STATUS:")))
		case strings.HasPrefix(line, "COMMENT:"):
			v.Comment = strings.TrimSpace(strings.TrimPrefix(line, "COMMENT:"))
		}
		i++
	}
	return v, i
}

// parseVerificationStatus normalizes a status string into a FindingStatus.
// Unknown values default to FindingStatusStillPresent to be conservative.
func parseVerificationStatus(raw string) FindingStatus {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "fixed":
		return FindingStatusFixed
	case "still_present":
		return FindingStatusStillPresent
	case "regressed":
		return FindingStatusRegressed
	default:
		return FindingStatusStillPresent
	}
}
```

### 3. Add `Verifications` field to `CycleState`

In `internal/loop/state.go`, add to `CycleState`:

```go
Verifications []FindingVerification // current cycle's verification results
```

### 4. Wire into `runReviewerPhase`

In `internal/loop/loop.go`, after the existing `ParseReviewFindings` call in `runReviewerPhase`:

```go
state.Findings = ParseReviewFindings(result.ResultText)
state.Verifications = ParseVerifications(result.ResultText)
```

## Files

- `internal/loop/state.go` — Add `FindingVerification` struct and `Verifications` field to `CycleState`
- `internal/loop/parse.go` — Add `ParseVerifications`, `parseVerificationBlock`, `parseVerificationStatus`
- `internal/loop/loop.go` — Wire `ParseVerifications` into `runReviewerPhase`
- `internal/loop/parse_test.go` — Table-driven tests for `ParseVerifications`:
  - Single verification block
  - Multiple verification blocks
  - Mixed with ISSUE: blocks (both are parsed independently)
  - Missing FINDING_ID (skipped)
  - Unknown STATUS defaults to `still_present`
  - Empty output (no verifications)

## Acceptance Criteria

- [ ] `ParseVerifications` correctly extracts `FINDING_ID`, `STATUS`, and `COMMENT` from `VERIFICATION:` blocks
- [ ] Unknown status strings default to `still_present`
- [ ] Blocks missing `FINDING_ID` are skipped
- [ ] `ParseVerifications` and `ParseReviewFindings` can coexist in the same output without interference
- [ ] `state.Verifications` is populated after `runReviewerPhase`
- [ ] `go test ./internal/loop/...` passes with all new table-driven tests
- [ ] `go vet ./...` clean

+++
id = "finding-serialization"
title = "Add FindingID and Status fields to ReviewFinding, implement serialization for prompt injection"
type = "feature"
priority = 1
labels = ["quasar", "reviewer", "quality"]
+++

## Problem

`ReviewFinding` in `internal/loop/state.go` only carries `Severity`, `Description`, and `Cycle`. There is no stable identifier to track a finding across cycles, and no status field to record whether it was fixed, still present, or regressed. The reviewer currently gets no memory of prior findings — it reviews from scratch each cycle, which means it can miss regressions and waste tokens re-discovering already-known issues.

## Solution

Extend `ReviewFinding` with an `ID` field (deterministic hash of severity + description) and a `Status` field representing the finding's lifecycle state. Add a serialization function that renders a slice of findings into a compact, prompt-friendly text block.

### 1. Add fields to `ReviewFinding`

In `internal/loop/state.go`:

```go
// FindingStatus represents the lifecycle state of a review finding.
type FindingStatus string

const (
	FindingStatusFound        FindingStatus = "found"
	FindingStatusFixed        FindingStatus = "fixed"
	FindingStatusStillPresent FindingStatus = "still_present"
	FindingStatusRegressed    FindingStatus = "regressed"
)

// ReviewFinding represents a single issue identified by the reviewer.
type ReviewFinding struct {
	ID          string        // deterministic hash for cross-cycle tracking
	Severity    string
	Description string
	Cycle       int           // cycle in which this finding was first created
	Status      FindingStatus // lifecycle status (set during verification)
}
```

### 2. Deterministic ID generation

In a new file `internal/loop/finding_id.go`:

```go
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
```

### 3. Serialization for prompt injection

In a new file `internal/loop/finding_serialize.go`:

```go
// SerializeFindings renders a slice of ReviewFinding into a compact text block
// suitable for injection into the reviewer prompt. Each finding is formatted as
// a numbered entry with its ID, severity, cycle of origin, current status, and
// a truncated description.
func SerializeFindings(findings []ReviewFinding, maxDescLen int) string {
	var b strings.Builder
	for i, f := range findings {
		desc := truncate(f.Description, maxDescLen)
		fmt.Fprintf(&b, "%d. [%s] id=%s cycle=%d status=%s\n   %s\n",
			i+1, f.Severity, f.ID, f.Cycle, f.Status, desc)
	}
	return b.String()
}
```

### 4. Assign IDs during parsing

Update `ParseReviewFindings` in `internal/loop/parse.go` to assign `ID` and initial `Status` to each finding:

```go
findings = append(findings, f)
// After appending, set:
findings[len(findings)-1].ID = FindingID(f.Severity, f.Description)
findings[len(findings)-1].Status = FindingStatusFound
```

## Files

- `internal/loop/state.go` — Add `FindingStatus` type, constants, and new fields (`ID`, `Status`) to `ReviewFinding`
- `internal/loop/finding_id.go` — New file: `FindingID()` deterministic hash function
- `internal/loop/finding_serialize.go` — New file: `SerializeFindings()` for prompt-ready text
- `internal/loop/parse.go` — Update `ParseReviewFindings` to assign `ID` and `Status = FindingStatusFound`
- `internal/loop/finding_id_test.go` — Tests for `FindingID` stability and uniqueness
- `internal/loop/finding_serialize_test.go` — Tests for `SerializeFindings` output format

## Acceptance Criteria

- [ ] `ReviewFinding` has `ID` and `Status` fields
- [ ] `FindingID` produces stable, deterministic IDs for the same severity+description
- [ ] `FindingID` produces different IDs for different inputs
- [ ] `SerializeFindings` renders a human-readable block with id, cycle, status, and truncated description
- [ ] `ParseReviewFindings` sets `ID` and `Status = "found"` on every returned finding
- [ ] `go test ./internal/loop/...` passes
- [ ] `go vet ./...` clean

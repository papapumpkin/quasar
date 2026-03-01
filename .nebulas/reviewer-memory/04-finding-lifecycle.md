+++
id = "finding-lifecycle"
title = "Apply verifications to update finding statuses and surface lifecycle in UI and bead comments"
type = "feature"
priority = 2
depends_on = ["verification-parsing"]
labels = ["quasar", "reviewer", "quality"]
+++

## Problem

After phase 03, `state.Verifications` is populated each cycle, but nothing applies those verifications to update the `Status` field on findings in `state.AllFindings`. The lifecycle data is captured but never acted upon — findings remain at `Status = "found"` forever, the UI shows no verification progress, and bead comments do not reflect whether issues were actually fixed.

## Solution

Add an `ApplyVerifications` function that matches verifications to findings by ID and updates their statuses. Wire it into the main loop after the reviewer phase. Surface the lifecycle summary in the UI and in bead comments.

### 1. `ApplyVerifications` in `internal/loop/finding_lifecycle.go`

```go
// ApplyVerifications matches verification results to accumulated findings by ID
// and updates their Status field. Returns counts of each status transition for
// UI reporting. Findings with no matching verification retain their current status.
func ApplyVerifications(allFindings []ReviewFinding, verifications []FindingVerification) LifecycleSummary {
	byID := make(map[string]*ReviewFinding, len(allFindings))
	for i := range allFindings {
		byID[allFindings[i].ID] = &allFindings[i]
	}

	summary := LifecycleSummary{}
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
```

### 2. Wire into `runLoop` in `internal/loop/loop.go`

After `runReviewerPhase` returns and before the `isApproved` check, apply verifications:

```go
if err := l.runReviewerPhase(ctx, state, perAgentBudget); err != nil {
	return nil, err
}

// Apply verification results to update finding lifecycle statuses.
if len(state.Verifications) > 0 {
	summary := ApplyVerifications(state.AllFindings, state.Verifications)
	l.UI.FindingLifecycle(state.Cycle, summary)
}
```

### 3. Add `FindingLifecycle` to `ui.UI` interface

Add a new method to the UI interface and implement it in the stderr printer:

```go
// FindingLifecycle reports the verification summary for a cycle.
FindingLifecycle(cycle int, summary LifecycleSummary)
```

Implementation in `internal/ui/printer.go`:

```go
func (p *Printer) FindingLifecycle(cycle int, summary loop.LifecycleSummary) {
	fmt.Fprintf(p.w, "  Findings: %s\n", summary.String())
}
```

### 4. Include lifecycle data in bead comments

Update `emitBeadUpdate` or the finding bead creation path to include status information. When emitting the `EventAgentDone` event for the reviewer, include the verification summary in the message:

```go
// In runReviewerPhase, enhance the event message:
verifyMsg := ""
if len(state.Verifications) > 0 {
	summary := ApplyVerifications(state.AllFindings, state.Verifications)
	verifyMsg = fmt.Sprintf("\n[verification] %s", summary.String())
}
l.emit(ctx, Event{
	Kind:    EventAgentDone,
	BeadID:  state.TaskBeadID,
	Cycle:   state.Cycle,
	Agent:   "reviewer",
	Result:  &result,
	Message: fmt.Sprintf("[reviewer cycle %d]\n%s%s", state.Cycle, truncate(result.ResultText, 2000), verifyMsg),
})
```

### 5. Filter out fixed findings from coder prompt

Update `buildCoderPrompt` in `internal/loop/prompts.go` so that when presenting findings on cycle 2+, findings with `Status == FindingStatusFixed` are omitted — the coder should only work on unresolved issues:

```go
// In buildCoderPrompt, when state.Cycle > 1:
var unresolved []ReviewFinding
for _, f := range state.Findings {
	if f.Status != FindingStatusFixed {
		unresolved = append(unresolved, f)
	}
}
// Use unresolved instead of state.Findings for the numbered list
```

## Files

- `internal/loop/finding_lifecycle.go` — New file: `ApplyVerifications`, `LifecycleSummary` type
- `internal/loop/loop.go` — Wire `ApplyVerifications` after reviewer phase; enhance reviewer event message
- `internal/loop/prompts.go` — Filter fixed findings from coder prompt
- `internal/ui/ui.go` — Add `FindingLifecycle` method to `UI` interface
- `internal/ui/printer.go` — Implement `FindingLifecycle` on `Printer`
- `internal/loop/finding_lifecycle_test.go` — Table-driven tests:
  - All findings verified as fixed
  - Mix of fixed, still_present, regressed
  - Verification with unknown finding ID (ignored)
  - Empty verifications (no changes)
  - `HasUnresolved` returns correct boolean
  - `String` format is correct

## Acceptance Criteria

- [ ] `ApplyVerifications` correctly updates `Status` on matching findings in `AllFindings`
- [ ] Unmatched verification IDs are silently ignored
- [ ] `LifecycleSummary.String()` produces readable output like "2 fixed, 1 still present, 0 regressed"
- [ ] `LifecycleSummary.HasUnresolved()` returns true when `StillPresent > 0` or `Regressed > 0`
- [ ] UI displays finding lifecycle summary after each reviewer cycle (cycle 2+)
- [ ] Bead comments for the reviewer include verification summary
- [ ] Coder prompt on cycle 2+ omits findings already marked `fixed`
- [ ] On cycle 1, no lifecycle logic runs (no verifications to apply)
- [ ] `go test ./internal/loop/...` passes
- [ ] `go vet ./...` clean

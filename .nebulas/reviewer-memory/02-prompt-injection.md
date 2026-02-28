+++
id = "prompt-injection"
title = "Inject prior findings into the reviewer prompt with verification instructions"
type = "feature"
priority = 1
depends_on = ["finding-serialization"]
labels = ["quasar", "reviewer", "quality"]
+++

## Problem

`buildReviewerPrompt` in `internal/loop/prompts.go` constructs the reviewer's prompt from only the current cycle's coder output and task description. The reviewer has no memory of what it found in previous cycles. This means it reviews from scratch each time, cannot verify whether the coder actually fixed prior issues, and may miss regressions introduced while fixing other findings.

## Solution

Modify `buildReviewerPrompt` to inject a `[PRIOR FINDINGS]` section when `state.AllFindings` is non-empty (i.e., cycle > 1). Include explicit instructions telling the reviewer to verify each prior finding and report its status using a structured `VERIFICATION:` block.

### 1. Update `buildReviewerPrompt` in `internal/loop/prompts.go`

After the existing review instructions, add a prior-findings section:

```go
func (l *Loop) buildReviewerPrompt(state *CycleState) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Task (bead %s): %s\n\n", state.TaskBeadID, state.TaskTitle)
	b.WriteString("The coder has completed their work. Here is their summary:\n\n")
	b.WriteString(truncate(state.CoderOutput, 3000))

	if state.LintOutput != "" {
		b.WriteString("\n\nNOTE: The following lint issues were not fully resolved by the coder:\n")
		b.WriteString(truncate(state.LintOutput, 2000))
	}

	b.WriteString("\n\nREVIEW INSTRUCTIONS:\n")
	b.WriteString("1. READ THE ACTUAL SOURCE FILES to verify the changes — do not rely solely on the summary above.\n")
	b.WriteString("2. Check for correctness, security, error handling, code quality, and edge cases.\n")
	b.WriteString("3. Check for any linting issues (`go vet`, `go fmt`). If linting problems exist, flag them as issues for the coder to fix.\n")
	b.WriteString("4. End your review with either APPROVED: or one or more ISSUE: blocks.\n")

	// Inject prior findings for verification when this is not the first cycle.
	if len(state.AllFindings) > 0 {
		b.WriteString("\n")
		b.WriteString(buildPriorFindingsBlock(state.AllFindings))
	}

	return b.String()
}
```

### 2. New helper `buildPriorFindingsBlock`

Add to `internal/loop/prompts.go`:

```go
// buildPriorFindingsBlock constructs the prior-findings section injected into
// the reviewer prompt on cycles > 1. It serializes all accumulated findings
// and adds explicit instructions for the reviewer to verify each one.
func buildPriorFindingsBlock(findings []ReviewFinding) string {
	var b strings.Builder
	b.WriteString("[PRIOR FINDINGS]\n")
	b.WriteString("The following issues were identified in previous review cycles.\n")
	b.WriteString("You MUST verify each one against the current code and report its status.\n\n")
	b.WriteString(SerializeFindings(findings, 200))
	b.WriteString("\nFor EACH prior finding, include a VERIFICATION: block in your response:\n\n")
	b.WriteString("VERIFICATION:\n")
	b.WriteString("FINDING_ID: <id from above>\n")
	b.WriteString("STATUS: fixed|still_present|regressed\n")
	b.WriteString("COMMENT: Brief explanation of what you observed.\n\n")
	b.WriteString("After verifying all prior findings, proceed with your normal review.\n")
	b.WriteString("Report any NEW issues as ISSUE: blocks (they will get new IDs automatically).\n")
	return b.String()
}
```

### 3. Update `DefaultReviewerSystemPrompt`

Add a note about the verification protocol to `internal/agent/reviewer.go` so the reviewer's system prompt primes it for the format:

```go
// Append to the existing prompt, after the REPORT block section:

## Finding Verification (Cycles > 1)

When a [PRIOR FINDINGS] section is present in the task prompt, you must verify
each listed finding against the current code. For each finding, emit:

VERIFICATION:
FINDING_ID: <the finding's id>
STATUS: fixed|still_present|regressed
COMMENT: What you observed in the current code.

"fixed" — the issue is fully resolved.
"still_present" — the issue remains unchanged.
"regressed" — the issue was partially fixed but introduced new problems, or a previously fixed issue has returned.
```

## Files

- `internal/loop/prompts.go` — Modify `buildReviewerPrompt` to inject prior findings; add `buildPriorFindingsBlock` helper
- `internal/agent/reviewer.go` — Append verification protocol to `DefaultReviewerSystemPrompt`
- `internal/loop/prompts_test.go` — Test that prior findings are injected on cycle > 1 but not on cycle 1; test the serialized format

## Acceptance Criteria

- [ ] On cycle 1 (empty `AllFindings`), the reviewer prompt is unchanged from current behavior
- [ ] On cycle 2+, the reviewer prompt includes a `[PRIOR FINDINGS]` block with all accumulated findings
- [ ] The `[PRIOR FINDINGS]` block includes each finding's ID, severity, cycle, status, and truncated description
- [ ] The prompt instructs the reviewer to emit `VERIFICATION:` blocks with `FINDING_ID:`, `STATUS:`, and `COMMENT:`
- [ ] `DefaultReviewerSystemPrompt` includes the verification protocol documentation
- [ ] `buildPriorFindingsBlock` truncates descriptions to 200 characters to control prompt size
- [ ] `go test ./internal/loop/...` passes
- [ ] `go vet ./...` clean

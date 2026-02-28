+++
id = "reviewer-escalation"
title = "Auto-escalate high-risk reviewer findings as hails"
type = "feature"
priority = 2
depends_on = ["hail-extraction", "ui-hail-interface"]
+++

## Problem

The reviewer's structured output (ISSUE blocks with SEVERITY + RECOMMENDATION) contains valuable signal about when human attention is needed, but currently this signal is only captured in the ReviewReport metadata. Critical and major issues with high risk should proactively reach the human rather than silently cycling.

Additionally, when the coder-reviewer loop reaches max cycles without approval, the human should know *why* — not just that it failed.

## Solution

### Automatic Escalation Rules
After parsing the reviewer's output, apply escalation rules:

1. **NEEDS_HUMAN_REVIEW: yes** → always create a hail (already planned in phase 02)
2. **Any critical severity issue** → create a `HailBlocker` with the issue detail
3. **RISK: high + SATISFACTION: low** → create a `HailDecisionNeeded` suggesting the human intervene
4. **Max cycles reached** → create a `HailBlocker` with the final reviewer summary and all unresolved issues

### Escalation in the UI
Escalated hails should be visually distinct from informational ones:
- Critical/blocker hails get red highlighting in the TUI
- Status bar badge differentiates: `⚠ 1 hail | 🔴 1 critical`

## Files

- `internal/loop/hail_extract.go` — Add escalation rules based on severity and report fields
- `internal/loop/loop.go` — Create hail on max-cycles-reached before returning error
- `internal/tui/statusbar.go` — Differentiate critical vs. normal hail counts
- `internal/tui/hail_overlay.go` — Color-code critical hails

## Acceptance Criteria

- [ ] Critical severity issues auto-escalate to hails
- [ ] High risk + low satisfaction triggers a hail
- [ ] Max cycles reached creates a blocker hail with context
- [ ] TUI distinguishes critical hails visually
- [ ] Escalation rules are testable and tested independently
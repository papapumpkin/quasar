+++
id = "hail-extraction"
title = "Extract hails from agent output after each cycle"
type = "feature"
priority = 2
depends_on = ["hail-types"]
+++

## Problem

Agents express blockers and questions in natural language within their output, but Quasar doesn't currently parse or act on them. The reviewer's `NEEDS_HUMAN_REVIEW: yes` flag is parsed into `ReviewReport` but doesn't trigger any interactive behavior — it's just metadata stored in the report.

We need the loop to detect when an agent is signaling "I need human help" and convert those signals into Hail objects.

## Solution

Add hail extraction logic that runs after each agent invocation. Two sources:

1. **Reviewer report parsing**: When `NEEDS_HUMAN_REVIEW: yes`, automatically create a `HailHumanReviewFlag` hail with the reviewer's summary and risk assessment as context.

2. **Discovery bridging**: When Fabric is enabled and an agent posts a `requirements_ambiguity` or `missing_dependency` discovery, the loop should also create a corresponding Hail so it surfaces in the UI.

Wire the HailQueue into the Loop struct and call extraction after `runCoderPhase` and `runReviewerPhase`.

## Files

- `internal/loop/hail_extract.go` — Extraction functions: `extractReviewerHails(report ReviewReport, state CycleState) []Hail` and `bridgeDiscoveryHails(discoveries []fabric.Discovery) []Hail`
- `internal/loop/loop.go` — Add `HailQueue` field to Loop struct; call extraction after agent phases

## Acceptance Criteria

- [ ] `NEEDS_HUMAN_REVIEW: yes` in reviewer output creates a hail automatically
- [ ] Discovery events of kind `requirements_ambiguity` and `missing_dependency` bridge to hails
- [ ] HailQueue is wired into Loop and populated during execution
- [ ] Hails include phase ID context when running in nebula mode
- [ ] Tests verify extraction from reviewer reports and discovery bridging
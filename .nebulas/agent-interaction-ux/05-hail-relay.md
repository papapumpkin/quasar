+++
id = "hail-relay"
title = "Relay hail resolutions back to agents on next cycle"
type = "feature"
priority = 2
depends_on = ["hail-extraction"]
+++

## Problem

When the human resolves a hail, that resolution needs to reach the agent on its next cycle. Currently, the only way context flows into the coder is through the task description (cycle 1) or reviewer findings (cycle 2+). There's no channel for "the human answered your question."

## Solution

When building the coder or reviewer prompt for a new cycle, check the HailQueue for recently resolved hails and inject them as context:

```
[HUMAN RESPONSE]
Your question about synchronization approach (cycle 3) was answered:
"Use channels — we prefer CSP style in this codebase."

Proceed with this guidance in mind.
```

Add this to `buildCoderPrompt` and `buildReviewerPrompt` in `internal/loop/prompts.go`. The injection should:
- Only include hails resolved since the last cycle
- Clear "relayed" flag after injection so hails aren't repeated
- Be concise — include summary, resolution, and cycle reference only

## Files

- `internal/loop/hail.go` — Add `RelayedAt` field to Hail; `UnrelayedResolved() []Hail` method
- `internal/loop/prompts.go` — Inject resolved hails into coder and reviewer prompts
- `internal/loop/loop.go` — Mark hails as relayed after prompt construction

## Acceptance Criteria

- [ ] Resolved hails appear in the next cycle's agent prompt
- [ ] Each resolution is relayed exactly once
- [ ] Relay context is concise and clearly attributed
- [ ] Works for both coder and reviewer prompt paths
- [ ] Tests verify injection and one-shot relay behavior
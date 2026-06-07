+++
name = "master-review-rubric"
tools_add = [
  "Bash(go vet *)",
  "Bash(go test *)",
]
+++

## Output schema: master-review-decision-v1

Emit exactly one JSON object — no prose before or after it — matching:

```json
{
  "verdict": "approve" | "request_changes" | "abandon",
  "score": 0,
  "reasons": [{ "category": "correctness|tests|safety|style|scope", "detail": "string" }],
  "suggestions": ["string"],
  "blocker": null
}
```

- `verdict` (required) — one of `approve`, `request_changes`, `abandon`.
- `score` (required) — an integer 0–100.
- `reasons` — each entry's `category` is one of `correctness`, `tests`,
  `safety`, `style`, `scope`. Empty when `verdict` is `approve`.
- `suggestions` — concrete, actionable changes. Empty when `verdict` is
  `approve`; these become the brief the architect uses to plan fix phases.
- `blocker` — non-null only when `verdict` is `abandon`; a one-line statement of
  the unrecoverable problem.

## Rubric

- **correctness** — does the change solve the stated problem? Read the nebula's
  `## Problem` and `## Acceptance Criteria`.
- **tests** — does every changed code path have a test that would fail before
  the change?
- **safety** — does the change respect the gitops perimeter? Any new
  `exec.Command("git", ...)` outside `internal/gitops` is a `safety` blocker.
- **style** — does the change follow CLAUDE.md conventions (interfaces defined
  consumer-side, explicit error handling, short focused functions)?
- **scope** — does the change stay within the nebula's `scope = [...]` globs?

Score weighting: correctness 40, tests 30, safety 20, style 5, scope 5.

`verdict = approve` requires `score >= 80` AND no `safety` reasons.
`verdict = abandon` is reserved for unrecoverable scope or architecture
violations; everything else recoverable is `request_changes`.

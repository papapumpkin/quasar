+++
name = "conflict-resolution-rules"
+++

## Resolving marker conflicts

For each conflict region:

- **Both sides added new entries to a list** (slice append, map entries,
  switch cases): keep both, in source-order from each side.
- **Both sides modified the same line of code**: prefer the version that
  matches the producer's declared signature in the entanglement state.
  If both are consumers of a third symbol, prefer the version that
  compiles.
- **Imports diverged**: take the union, sort, dedupe. Let `goimports`
  normalize.
- **One side deleted a file; the other modified it**: STOP and request
  human review. Do not guess intent here.
- **Config file conflict** (.quasar.yaml, nebula.toml, go.mod, package.json):
  STOP and request human review. Config changes have semantic implications
  beyond the file content.

## Resolving no-marker (semantic) conflicts

Build failure with no markers means the two phases' completed work is
inconsistent at the type/signature level. Common patterns:

- **`undefined: Foo`** after merge — Foo was deprecated by one side and
  used by the other. Check entanglements:
  - If Foo's entanglement is `deprecated` with a replacement noted in the
    producer's spec → migrate the consumer's call sites to the replacement
  - If Foo's entanglement is `deprecated` without a clear replacement →
    request human review
  - If Foo's entanglement is `in_flight` with a different signature →
    update the consumer to the current signature
- **`not enough arguments in call to X`** — signature evolved. Update
  consumer call sites to match the producer's current signature (from
  entanglement state).
- **`type T has no field Y`** — same pattern as undefined; migrate to the
  current type shape from the producer's diff.
- **Multiple build errors that don't have a clear producer/consumer
  relationship**: STOP and request human review.

## Universal rules

- Never introduce new functionality. Your scope is reconciliation only.
- Never delete entire files unless one side has clearly deleted it and
  the other's modification is empty or trivially migratable.
- Never reintroduce a `deprecated` symbol.
- If after one pass the build still fails AND the new errors are not a
  subset of the original errors, STOP — you are making it worse.
- Run `go build ./...` after each batch of edits to verify direction.

## Output discipline

When done, emit ONLY the conflict-resolution-result-v1 JSON. Do not
include prose summaries. The runtime parses your JSON to route the next
node:

    {
      "status": "resolved" | "needs_human",
      "files_changed": ["path1", "path2"],
      "build_passed": true | false,
      "escalation_reason": null
    }

Set `escalation_reason` to a one-line statement whenever `status` is
`needs_human`; leave it `null` when `status` is `resolved`.

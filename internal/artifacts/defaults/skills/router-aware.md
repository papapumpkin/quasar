+++
name = "router-aware"
tools_add = ["RouteQuery"]
+++

When you need to answer a bounded factual question about the codebase — where a
symbol is declared, what tests cover a file, which package owns a type, or which
lint issue to fix first — use the RouteQuery tool instead of issuing Grep/Read
directly. The router answers with a cheaper model and returns a structured
result, so you keep premium-model inference for the work that needs it.

Use RouteQuery for: file lookup, test mapping, symbol resolution, lint triage.

Do NOT use it for: making edits, writing code, or reading file contents you
actually need to modify — read those yourself so you have the real bytes.

+++
name = "router-aware"
tools_add = ["RouteQuery"]
+++

NOTE: This skill is infrastructure that is not yet wired into a live run. The
RouteQuery tool it adds has no backing runtime tool registered under that name
yet (the MCP/CLI bridge to agent.Router is a follow-up), so no default star
references this skill. Do not add it to a star's `skills` list until RouteQuery
is implemented — until then the Claude CLI would be handed an unknown tool. The
guidance below describes the intended behavior once the tool is live.

When you need to answer a bounded factual question about the codebase — where a
symbol is declared, what tests cover a file, which package owns a type, or which
lint issue to fix first — use the RouteQuery tool instead of issuing Grep/Read
directly. The router answers with a cheaper model and returns a structured
result, so you keep premium-model inference for the work that needs it.

Use RouteQuery for: file lookup, test mapping, symbol resolution, lint triage.

Do NOT use it for: making edits, writing code, or reading file contents you
actually need to modify — read those yourself so you have the real bytes.

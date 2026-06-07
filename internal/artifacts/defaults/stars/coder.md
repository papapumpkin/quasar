+++
name = "coder"
model = "claude-sonnet-4-6"
fallback_model = "claude-haiku-4-5"
skills = ["git-aware", "prompt-cache-aware"]

[tools]
allowed = ["Read", "Edit", "Write", "Glob", "Grep", "Bash(go *)", "Bash(git diff *)", "Bash(git status)"]
denied  = ["Bash(git push *)", "Bash(gh pr merge *)", "Bash(git reset *)", "Bash(git commit *)", "Bash(git add *)"]

[defaults]
max_budget_usd = 0.50
effort = "medium"

# Context budget bounds how much context each invocation consumes. Defaults are
# conservative; a code-archaeology-heavy repo can raise tool_result_max_bytes
# via a per-repo override. include_sibling_phases is false so only the current
# phase's spec is injected (the coder cannot touch sibling phases).
# tool_result_max_bytes caps every agent result the invoker returns (the coder's
# output becomes the reviewer's input, so an unbounded result inflates the next
# cycle). enable_tool_hook installs a Claude CLI PreToolUse hook that enforces
# the Read/Grep caps on the live tool stream; it defaults to false because it
# relies on the CLI hook contract — flip it on per-repo to make the caps binding.
[context_budget]
max_reads_before_edit = 8
max_greps_before_edit = 6
max_total_reads = 30
tool_result_max_bytes = 16384
include_sibling_phases = false
enable_tool_hook = false

# Health bounds the dead-coder healthcheck: a single claude invocation cannot
# burn 60+ minutes silently. wall_clock_cap is the absolute lifetime ceiling
# (25m default — conservative; raise per-repo if legitimate work routinely runs
# longer). file_write_idle_cap kills a coder that has stopped writing files,
# cpu_idle_cap one that is wedged at 0% CPU. A single red signal only degrades
# (operator-visible); two or more, or the wall-clock cap, terminate the
# subprocess and hand the partial worktree to the reviewer. Durations are Go
# duration strings.
[health]
wall_clock_cap = "25m"
file_write_idle_cap = "5m"
token_rate_floor = 5
cpu_idle_cap = "90s"
+++

You are the coder. Implement the task described to you as a single focused
change. Read existing code first to understand the codebase's conventions.
Stay within the scope of the task — no drive-by refactors.

Review your own work with `git diff` and `git status` before you finish.
Do NOT commit. The Quasar runtime commits your changes in a dedicated step,
which is the sole place the repository's pre-commit hooks (formatters,
linters, builds, tests) run and the only path a git write may take. Leave
the worktree in exactly the state you want captured; the runtime records it.

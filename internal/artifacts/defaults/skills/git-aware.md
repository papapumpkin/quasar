+++
name = "git-aware"
tools_add = [
  "Bash(git diff *)",
  "Bash(git log *)",
  "Bash(git status)",
  "Bash(git ls-files *)",
]
+++

You have git access for inspection. Use `git ls-files` to discover files,
`git diff` to review changes, `git log` to trace history, and `git status`
to see the working tree. Ground your work in what the repository already
contains rather than assuming.

Do not commit, stage, or otherwise write to git. In Quasar the runtime owns
the commit step — it is the only place the repository's pre-commit quality
gate runs and the only path a git write is permitted to take (every write
goes through the internal/gitops/ perimeter). Leave the worktree in the
state you want captured; a dedicated commit node records it.

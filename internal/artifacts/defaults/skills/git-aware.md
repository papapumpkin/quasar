+++
name = "git-aware"
tools_add = [
  "Bash(git diff *)",
  "Bash(git log *)",
  "Bash(git status)",
  "Bash(git add *)",
  "Bash(git commit *)",
  "Bash(git ls-files *)",
]
+++

You have git access. Inspect existing code with `git ls-files`, check
diffs with `git diff`, commit changes with clear, imperative-mood messages
under 72 chars. Prefer small commits over large ones — each commit should
answer one question.

The repository's pre-commit hooks run automatically when you commit.
If a hook modifies files (e.g. a formatter), those changes are staged
into your commit. If a hook fails, your commit aborts — address the
failure and commit again.

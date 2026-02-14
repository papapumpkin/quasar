+++
id = "named-constants"
title = "Replace magic numbers with named constants"
depends_on = ["fix-truncate"]
+++

Replace the magic numbers `2000`, `3000`, and `80` in `internal/loop/loop.go`
with named constants defined at the top of the file:

- `maxOutputLen = 2000`
- `maxPromptLen = 3000`
- `maxLineWidth = 80`

Update all call sites of `truncate()` to use these constants.

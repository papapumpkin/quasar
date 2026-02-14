+++
id = "fix-truncate"
title = "Fix truncate to use rune counting"
type = "bug"
priority = 1
+++

Fix the `truncate()` function in `internal/loop/loop.go` to count runes instead
of bytes so that multi-byte UTF-8 characters are not split mid-character.

Use `utf8.RuneCountInString` for the length check and `[]rune` conversion for
slicing. Keep the `... [truncated]` suffix.

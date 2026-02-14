+++
id = "truncate-tests"
title = "Add edge-case tests for truncate()"
depends_on = ["fix-truncate"]
+++

Add test cases for the `truncate()` function in `internal/loop/loop_test.go`
covering:

1. Empty string input
2. Input exactly at the truncation boundary (len == maxLen)
3. Input one byte over the boundary
4. Input containing multi-byte UTF-8 characters (e.g. emoji or CJK)
5. Input with emoji that would be split at a byte boundary

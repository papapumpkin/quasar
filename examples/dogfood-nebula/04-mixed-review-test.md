+++
id = "mixed-review-test"
title = "Test ParseReviewFindings with mixed APPROVED + ISSUE blocks"
+++

Add a test case in `internal/loop/loop_test.go` for `ParseReviewFindings` that
verifies correct parsing when both an `APPROVED:` block and one or more `ISSUE:`
blocks appear in the same review output. The function should still find the
issues even when `APPROVED:` is present.

+++
id = "ui-tests"
title = "Add unit tests for ui.Printer methods"
+++

Add unit tests in `internal/ui/ui_test.go` for the `ui.Printer` methods
(`Banner`, `Error`, `Info`, `TaskStarted`, `TaskComplete`) verifying that all
output is written to stderr, not stdout.

Use `os.Pipe()` to capture stderr output for assertions.

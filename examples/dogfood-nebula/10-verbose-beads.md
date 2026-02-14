+++
id = "verbose-beads"
title = "Log beads command output in verbose mode"
+++

In `internal/beads/beads.go`, add verbose logging that logs the stdout/stderr
results of beads commands (not just the command invocation), gated behind the
existing `Verbose` flag.

Currently only the command invocation is logged. After this change, the response
should also be logged when `Verbose` is true.

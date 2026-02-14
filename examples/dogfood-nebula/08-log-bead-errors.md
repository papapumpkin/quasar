+++
id = "log-bead-errors"
title = "Log warnings for silently dropped bead errors in the loop"
depends_on = ["named-constants"]
+++

In `internal/loop/loop.go`, the calls to `Beads.Close()` and
`Beads.AddComment()` discard errors with `_ =`. Replace these with calls to
`l.UI.Info()` or a new `l.UI.Warn()` method that logs the error so operators
can see when bead updates fail.

Only change the places where errors are currently silenced — do not change error
handling for `Beads.Create()` or `Beads.Update()` which already handle errors.

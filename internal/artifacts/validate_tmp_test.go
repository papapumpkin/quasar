//go:build ignore

// This file is an inert leftover. The real embedded-defaults parse test lives in
// builtins_test.go. This file could not be deleted because the working session
// blocked file removal (rm/mv/git clean); the //go:build ignore tag excludes it
// from all builds and tests so it cannot collide with builtins_test.go. Safe to
// `git rm` once outside the restricted session.
package artifacts

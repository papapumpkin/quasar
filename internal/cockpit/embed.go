//go:build cockpit

// Package-level embed of the built React cockpit bundle. This file compiles
// only under the `cockpit` build tag; without the tag, embed_disabled.go
// supplies a no-op so the binary stays lean and exposes no UI surface.
package cockpit

import (
	"embed"
	"io/fs"
)

// bundleFS holds the built React cockpit static bundle. The //go:embed
// directive requires a package-level var. It is read-only after package init —
// the embedded FS is never reassigned.
//
//go:embed all:dist
var bundleFS embed.FS

// bundle returns the embedded static bundle rooted at the build output dir, or
// nil if the embed is empty. nil makes the server serve no bundle and 404 the
// asset routes.
func bundle() fs.FS {
	sub, err := fs.Sub(bundleFS, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}

// bundleBuilt reports whether the binary was compiled with the cockpit bundle.
func bundleBuilt() bool { return true }

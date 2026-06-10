//go:build cockpit

package cockpit

import (
	"embed"
	"io/fs"
)

// assetsFS holds the built CSS and the vendored Datastar runtime. It is only
// compiled into the binary under the `cockpit` build tag; see embed_disabled.go
// for the no-tag fallback.
//
//go:embed assets/cockpit.css assets/datastar.js
var assetsFS embed.FS

// Assets returns the embedded cockpit assets rooted at the assets/ directory, so
// the /assets/ file server (which StripPrefixes "/assets/") resolves
// "cockpit.css" / "datastar.js" directly. Returning fs.FS (not embed.FS) keeps
// the signature identical to the no-tag fallback in embed_disabled.go.
func Assets() fs.FS {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		// Unreachable: the embedded assets/ directory always exists.
		return assetsFS
	}
	return sub
}

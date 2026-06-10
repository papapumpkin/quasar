//go:build cockpit

package cockpit

import "embed"

// assetsFS holds the built CSS and the vendored Datastar runtime. It is only
// compiled into the binary under the `cockpit` build tag; see embed_disabled.go
// for the no-tag fallback.
//
//go:embed assets/cockpit.css assets/datastar.js
var assetsFS embed.FS

// Assets returns the embedded cockpit asset filesystem (cockpit.css, datastar.js).
func Assets() embed.FS { return assetsFS }

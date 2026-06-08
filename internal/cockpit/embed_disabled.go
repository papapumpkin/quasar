//go:build !cockpit

package cockpit

import "io/fs"

// bundle returns nil because the cockpit bundle was not compiled in (the
// `cockpit` build tag was absent). The server serves no static assets and the
// "/" and asset routes 404.
func bundle() fs.FS { return nil }

// bundleBuilt reports whether the binary was compiled with the cockpit bundle.
func bundleBuilt() bool { return false }

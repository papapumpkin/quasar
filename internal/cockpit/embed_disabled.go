//go:build !cockpit

package cockpit

import "io/fs"

// Assets returns an empty filesystem. The real assets are embedded only under
// the `cockpit` build tag (see embed.go), so a default `go build` carries none
// of the cockpit's static files.
func Assets() fs.FS { return emptyFS{} }

// emptyFS is an fs.FS with no entries.
type emptyFS struct{}

func (emptyFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }

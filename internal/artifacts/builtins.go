// Package artifacts holds Quasar's built-in default constellations, stars, and
// skills, embedded into the binary so a freshly installed Quasar runs the full
// ticket-to-PR happy path with zero configuration files on disk.
//
// The defaults are also the canonical authoring examples for each artifact
// type. Per-repo files override the embedded default of the same name: the
// loader consults <repo>/constellations/<name>.toml (and stars/, skills/)
// first, then falls back to DefaultsFS. Sensors have no embedded defaults —
// only their Go types are built in — so defaults/sensors/ contains
// documentation only.
package artifacts

import "embed"

// DefaultsFS embeds the built-in artifact files under defaults/, laid out as
// defaults/{constellations,stars,skills,sensors}/. The directory name is the
// type discriminator: a file is a star because it lives in stars/, a
// constellation because it lives in constellations/.
//
// Consume it through the loader rather than reading paths directly. It is
// read-only after package init — the embedded FS is never reassigned.
//
//go:embed all:defaults
var DefaultsFS embed.FS

package artifacts

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"

	toml "github.com/pelletier/go-toml/v2"
)

// EmbeddedPath is the sentinel a PathResolver returns when no per-repo override
// file exists and the embedded default should be used. It mirrors
// repos.EmbeddedPath by value: artifacts must not import repos (a layering
// inversion), so the literal is duplicated here and the two stay in sync.
const EmbeddedPath = ":embedded:"

// PathResolver locates per-repo artifact override files by name, returning
// EmbeddedPath when no override exists so the loader falls back to the embedded
// default. It is defined here, where it is consumed (per the project's
// "interfaces where consumed" rule); *repos.Resolver satisfies it structurally,
// keeping the dependency arrow pointing down from repos to artifacts.
type PathResolver interface {
	RepoPath() string
	ConstellationPath(name string) string
	StarPath(name string) string
	SkillPath(name string) string
	SensorPath(name string) (string, error)
	AllSensorPaths() ([]string, error)
}

// read returns a file's bytes and a display source path. When resolverPath is
// the EmbeddedPath sentinel it reads from the embedded defaults; otherwise it
// reads the per-repo override from disk.
func (l *Loader) read(resolverPath, dir, name, ext string) ([]byte, string, error) {
	if resolverPath == EmbeddedPath {
		p := path.Join(embeddedRoot, dir, name+ext)
		data, err := fs.ReadFile(l.builtins, p)
		if err != nil {
			return nil, "", fmt.Errorf("artifacts: no %s named %q (no per-repo override and no embedded default)", dir, name)
		}
		return data, EmbeddedPath + p, nil
	}
	data, err := os.ReadFile(resolverPath)
	if err != nil {
		return nil, "", fmt.Errorf("artifacts: read %q: %w", resolverPath, err)
	}
	return data, resolverPath, nil
}

// decodeTOML unmarshals data into v, honoring the loader's Strict setting, and
// rewrites any go-toml decode error into a file:line:col diagnostic. lineOffset
// shifts reported rows for TOML embedded in Markdown frontmatter.
func (l *Loader) decodeTOML(data []byte, src string, lineOffset int, v any) error {
	dec := toml.NewDecoder(bytes.NewReader(data))
	if l.Strict {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(v); err != nil {
		return positionedError(err, src, lineOffset)
	}
	return nil
}

// positionedError converts a go-toml decode/strict error into an error string
// carrying file:line:col. The frontmatter line offset maps a position within an
// extracted frontmatter block back to its line in the original Markdown file.
func positionedError(err error, src string, lineOffset int) error {
	var de *toml.DecodeError
	if errors.As(err, &de) {
		row, col := de.Position()
		return fmt.Errorf("%s:%d:%d: %s", src, row+offsetRows(lineOffset), col, de.Error())
	}
	var sme *toml.StrictMissingError
	if errors.As(err, &sme) && len(sme.Errors) > 0 {
		first := sme.Errors[0]
		row, col := first.Position()
		return fmt.Errorf("%s:%d:%d: unknown field %q", src, row+offsetRows(lineOffset), col, first.Error())
	}
	return fmt.Errorf("%s: %w", src, err)
}

// offsetRows converts a 1-based frontmatter start line into the row delta to add
// to a position reported relative to the frontmatter block.
func offsetRows(fmStartLine int) int {
	if fmStartLine <= 0 {
		return 0
	}
	return fmStartLine - 1
}

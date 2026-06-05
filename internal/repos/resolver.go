package repos

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/papapumpkin/quasar/internal/config"
)

// EmbeddedPath is the sentinel returned when a per-repo override file does not
// exist. Callers pass it to the file loader (a later phase), which resolves the
// name against the embedded built-in FS instead.
const EmbeddedPath = ":embedded:"

// File-tree layout for per-repo overrides. Constellations and sensors are TOML;
// stars and skills are Markdown with TOML frontmatter (SKILL.md compatible).
const (
	constellationsDir = "constellations"
	starsDir          = "stars"
	skillsDir         = "skills"
	sensorsDir        = "sensors"

	tomlExt = ".toml"
	mdExt   = ".md"
)

// RepoConfig is the parsed .quasar.yaml for a repo. It is the same typed Config
// used throughout Quasar; aliased here to give the resolver API a focused name.
type RepoConfig = config.Config

// Resolver resolves per-repo configuration. Given a registered repo, it loads
// the repo's .quasar.yaml and provides layered lookup for
// constellation/star/skill/sensor files: a per-repo override wins, otherwise the
// embedded built-in default is used (EmbeddedPath). Sensors have no embedded
// default.
type Resolver struct {
	repo Repo
	cfg  RepoConfig
}

// NewResolver loads the repo's .quasar.yaml (if present) and returns a resolver.
// A missing .quasar.yaml is not an error — the resolver falls back to default
// config so a repo can rely entirely on built-ins. A malformed config is an
// error.
func NewResolver(repo *Repo) (*Resolver, error) {
	if repo == nil {
		return nil, fmt.Errorf("repos: nil repo")
	}

	r := &Resolver{repo: *repo}

	cfgPath := filepath.Join(repo.Path, ".quasar.yaml")
	if _, err := os.Stat(cfgPath); err == nil {
		cfg, loadErr := config.LoadFromPath(cfgPath)
		if loadErr != nil {
			return nil, fmt.Errorf("repos: load config for %q: %w", repo.Path, loadErr)
		}
		r.cfg = cfg
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("repos: stat config for %q: %w", repo.Path, err)
	}

	return r, nil
}

// Config returns the parsed .quasar.yaml for this repo (zero-valued defaults
// when the file is absent).
func (r *Resolver) Config() RepoConfig {
	return r.cfg
}

// ConstellationPath returns the per-repo override path for the named
// constellation if <repo>/constellations/<name>.toml exists, otherwise
// EmbeddedPath.
func (r *Resolver) ConstellationPath(name string) string {
	return r.overrideOrEmbedded(constellationsDir, name, tomlExt)
}

// StarPath mirrors ConstellationPath for stars (<repo>/stars/<name>.md).
func (r *Resolver) StarPath(name string) string {
	return r.overrideOrEmbedded(starsDir, name, mdExt)
}

// SkillPath mirrors ConstellationPath for skills (<repo>/skills/<name>.md).
func (r *Resolver) SkillPath(name string) string {
	return r.overrideOrEmbedded(skillsDir, name, mdExt)
}

// SensorPath returns the per-repo sensor file <repo>/sensors/<name>.toml, or
// ErrSensorNotConfigured if it does not exist. Sensors have no embedded default.
func (r *Resolver) SensorPath(name string) (string, error) {
	path := filepath.Join(r.repo.Path, sensorsDir, name+tomlExt)
	if fileExists(path) {
		return path, nil
	}
	return "", fmt.Errorf("%w: %s", ErrSensorNotConfigured, name)
}

// AllSensorPaths returns absolute paths to every <repo>/sensors/*.toml file,
// sorted. A missing sensors directory yields an empty slice (not an error).
func (r *Resolver) AllSensorPaths() ([]string, error) {
	dir := filepath.Join(r.repo.Path, sensorsDir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("repos: read sensors dir for %q: %w", r.repo.Path, err)
	}

	var paths []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != tomlExt {
			continue
		}
		paths = append(paths, filepath.Join(dir, e.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}

// overrideOrEmbedded returns the per-repo override path when it exists,
// otherwise EmbeddedPath.
func (r *Resolver) overrideOrEmbedded(dir, name, ext string) string {
	path := filepath.Join(r.repo.Path, dir, name+ext)
	if fileExists(path) {
		return path
	}
	return EmbeddedPath
}

// fileExists reports whether path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

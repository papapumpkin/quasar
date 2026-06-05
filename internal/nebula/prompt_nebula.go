package nebula

import (
	"context"
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/papapumpkin/quasar/internal/agent"
)

// defaultNebulaBudgetUSD is the budget cap applied to the architect invocation
// when refining a seed nebula. The seed-driven entry point does not (yet)
// thread a configurable budget; callers needing finer control adjust the
// manifest after generation.
const defaultNebulaBudgetUSD = 50.0

// maxNebulaName bounds the generated nebula directory name length.
const maxNebulaName = 40

//go:embed prompt_nebula.tmpl
var nebulaPromptSource string

// nebulaPromptTmpl is parsed once at package initialization so the template is
// not re-parsed on every RenderNebulaPrompt call. The join function is bound to
// strings.Join for rendering label lists.
var nebulaPromptTmpl = template.Must(
	template.New("nebula-prompt").
		Funcs(template.FuncMap{"join": strings.Join}).
		Parse(nebulaPromptSource),
)

// Generated identifies a nebula generated from a seed and written to disk.
type Generated struct {
	Name   string          // generated directory name (e.g. "nebula-fix-truncate")
	Path   string          // path to the written nebula directory
	Result *GenerateResult // full generation result for callers needing detail
}

// SeedNebula is the minimal view of a draft (seed) nebula row the architect
// reads back to refine into phases. It mirrors the nebulas-table columns the
// sensor populated: name/description/source provenance and the derived
// goals/constraints. It carries NO phases — producing those is the architect's
// job.
type SeedNebula struct {
	Name        string
	Description string
	SourceName  string
	SourceID    string
	SourceURL   string
	Goals       []string
	Constraints []string
	Labels      []string
	Assignee    string
}

// SeedNebulaReader reads a seed nebula by id. The fabric NebulaStore satisfies
// it via a thin adapter; tests inject a fake. The interface is defined here, at
// the point of consumption, so this package does not depend on fabric directly.
type SeedNebulaReader interface {
	ReadSeedNebula(ctx context.Context, nebulaID string) (*SeedNebula, error)
}

// FromNebula refines an existing seed nebula into an executable plan. The sensor
// has already written the seed row to SQLite; FromNebula reads it back via
// reader, renders it into the architect's prompt (see prompt_nebula.tmpl),
// invokes the LLM through the shared generation back-half, and writes the
// resulting nebula directory under outDir.
//
// The directory name is derived from the seed (see slugifyNebula). The returned
// Generated carries that name and the on-disk path; the manifest records the
// seed's SourceName and SourceID so provenance is preserved. WriteNebula refuses
// to overwrite, so the target directory must not already exist.
func FromNebula(ctx context.Context, invoker agent.Invoker, reader SeedNebulaReader, nebulaID, outDir string) (*Generated, error) {
	if reader == nil {
		return nil, fmt.Errorf("from nebula requires a non-nil seed reader")
	}
	if nebulaID == "" {
		return nil, fmt.Errorf("from nebula requires a non-empty nebula id")
	}
	if outDir == "" {
		return nil, fmt.Errorf("from nebula requires a non-empty output directory")
	}

	seed, err := reader.ReadSeedNebula(ctx, nebulaID)
	if err != nil {
		return nil, fmt.Errorf("read seed nebula %q: %w", nebulaID, err)
	}
	if seed == nil {
		return nil, fmt.Errorf("seed nebula %q not found", nebulaID)
	}

	prompt, err := RenderNebulaPrompt(seed)
	if err != nil {
		return nil, fmt.Errorf("rendering nebula prompt: %w", err)
	}

	name := slugifyNebula(seed)
	targetDir := filepath.Join(outDir, name)

	req := GenerateRequest{
		UserPrompt:   seed.Name,
		NebulaName:   name,
		OutputDir:    targetDir,
		MaxBudgetUSD: defaultNebulaBudgetUSD,
	}

	manifest := buildManifest(req)
	manifest.Nebula.SourceName = seed.SourceName
	manifest.Nebula.SourceID = seed.SourceID

	result, err := runGenerate(ctx, invoker, req, manifest, prompt)
	if err != nil {
		return nil, err
	}
	if result.Nebula != nil {
		result.Nebula.SourceName = seed.SourceName
		result.Nebula.SourceID = seed.SourceID
	}

	if err := WriteNebula(result, targetDir, WriteOptions{}); err != nil {
		return nil, fmt.Errorf("writing nebula %q: %w", name, err)
	}

	return &Generated{Name: name, Path: targetDir, Result: result}, nil
}

// RenderNebulaPrompt produces the architect's user prompt for a seed nebula. It
// is kept separate from the generation flow so it can be unit-tested without any
// LLM dependency.
func RenderNebulaPrompt(seed *SeedNebula) (string, error) {
	if seed == nil {
		return "", fmt.Errorf("cannot render prompt for nil seed nebula")
	}
	var b strings.Builder
	if err := nebulaPromptTmpl.Execute(&b, seed); err != nil {
		return "", fmt.Errorf("executing nebula prompt template: %w", err)
	}
	return b.String(), nil
}

// slugifyNebula derives a nebula directory name from a seed:
// "nebula-<slug-of-source-id>-<slug-of-name>", falling back to the name alone
// when there is no source id. The result is lowercase ASCII, hyphen-separated,
// and capped at maxNebulaName characters.
func slugifyNebula(seed *SeedNebula) string {
	ref := slugify(seed.SourceID)
	if ref == "" {
		ref = "seed"
	}

	name := "nebula-" + ref
	if nameSlug := slugify(seed.Name); nameSlug != "" {
		name += "-" + nameSlug
	}

	if len(name) > maxNebulaName {
		name = strings.TrimRight(name[:maxNebulaName], "-")
	}
	return name
}

// slugify reduces an arbitrary string to a lowercase, ASCII, hyphen-separated
// slug. Runs of non-alphanumeric characters (spaces, colons, slashes, '#',
// non-ASCII runes, etc.) collapse to a single hyphen, and leading/trailing
// hyphens are trimmed.
func slugify(s string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevHyphen = false
			continue
		}
		if !prevHyphen {
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	return strings.Trim(b.String(), "-")
}

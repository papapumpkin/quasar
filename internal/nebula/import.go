package nebula

import (
	"context"
	"errors"
	"fmt"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// ErrNoRepoPath is returned by ImportNebula when no repo_path is supplied.
// Every SQLite-backed nebula must belong to a registered repo.
var ErrNoRepoPath = errors.New("nebula: import requires a non-empty repo path")

// Inserter is the subset of *fabric.NebulaStore that ImportNebula needs.
// It is declared here, where it is consumed, so the importer can be tested
// against a fake and the nebula package stays decoupled from the full store.
type Inserter interface {
	Insert(ctx context.Context, n fabric.NebulaRow) (string, error)
	InsertPhase(ctx context.Context, nebulaID string, phase fabric.PhaseRow) error
}

// ImportNebula converts a parsed in-memory Nebula into a row in the
// SQLite-backed store, inserting the nebula and each of its phases. It is the
// bridge from the on-disk authoring format (used by `quasar nebula apply` and
// import-on-first-run) to the canonical execution-time store. The on-disk files
// are not touched; this only writes to the database and blobstore. It returns
// the generated nebula id.
func ImportNebula(ctx context.Context, store Inserter, n *Nebula, repoPath string) (string, error) {
	if n == nil {
		return "", fmt.Errorf("nebula: import: nil nebula")
	}
	if repoPath == "" {
		return "", ErrNoRepoPath
	}

	row, phases, err := nebulaToRows(n, repoPath)
	if err != nil {
		return "", err
	}

	id, err := store.Insert(ctx, row)
	if err != nil {
		return "", fmt.Errorf("nebula: import insert: %w", err)
	}
	for _, p := range phases {
		if err := store.InsertPhase(ctx, id, p); err != nil {
			return "", fmt.Errorf("nebula: import phase %q: %w", p.ID, err)
		}
	}
	return id, nil
}

// nebulaToRows maps a parsed Nebula onto the store's NebulaRow plus one PhaseRow
// per phase. Manifest blocks are rendered to TOML; phase frontmatter is rendered
// from the serialization-only subset so Body/SourceFile are excluded.
func nebulaToRows(n *Nebula, repoPath string) (fabric.NebulaRow, []fabric.PhaseRow, error) {
	defaultsTOML, err := marshalTOML(n.Manifest.Defaults)
	if err != nil {
		return fabric.NebulaRow{}, nil, fmt.Errorf("nebula: render defaults: %w", err)
	}
	executionTOML, err := marshalTOML(n.Manifest.Execution)
	if err != nil {
		return fabric.NebulaRow{}, nil, fmt.Errorf("nebula: render execution: %w", err)
	}
	contextTOML, err := marshalTOML(n.Manifest.Context)
	if err != nil {
		return fabric.NebulaRow{}, nil, fmt.Errorf("nebula: render context: %w", err)
	}

	srcName, srcID := resolveSource(n)
	row := fabric.NebulaRow{
		RepoPath:      repoPath,
		Name:          n.Manifest.Nebula.Name,
		Description:   n.Manifest.Nebula.Description,
		SourceName:    srcName,
		SourceID:      srcID,
		DefaultsTOML:  defaultsTOML,
		ExecutionTOML: executionTOML,
		ContextTOML:   contextTOML,
	}

	phases := make([]fabric.PhaseRow, 0, len(n.Phases))
	for i, p := range n.Phases {
		fm, err := marshalPhaseFrontmatter(p)
		if err != nil {
			return fabric.NebulaRow{}, nil, fmt.Errorf("nebula: render phase %q frontmatter: %w", p.ID, err)
		}
		phases = append(phases, fabric.PhaseRow{
			ID:              p.ID,
			Seq:             i,
			Title:           p.Title,
			Body:            p.Body,
			FrontmatterTOML: fm,
		})
	}
	return row, phases, nil
}

// resolveSource selects the source attribution as an atomic (name, id) pair:
// the top-level Nebula fields win when a top-level SourceName is set, otherwise
// the manifest's [nebula] source fields are used together. Name and id are never
// mixed across the two origins, since they always travel as a unit.
func resolveSource(n *Nebula) (name, id string) {
	if n.SourceName != "" {
		return n.SourceName, n.SourceID
	}
	return n.Manifest.Nebula.SourceName, n.Manifest.Nebula.SourceID
}

// marshalTOML renders v to a TOML string.
func marshalTOML(v any) (string, error) {
	b, err := toml.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// marshalPhaseFrontmatter renders a phase's frontmatter (the serialization-only
// subset, excluding Body and SourceFile) to a TOML string. It shares
// newPhaseSpecFrontmatter with MarshalPhaseFile so both paths serialize the
// same fields.
func marshalPhaseFrontmatter(spec PhaseSpec) (string, error) {
	return marshalTOML(newPhaseSpecFrontmatter(spec))
}

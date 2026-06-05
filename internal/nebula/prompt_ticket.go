package nebula

import (
	"context"
	_ "embed"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/integrations"
)

// defaultTicketBudgetUSD is the budget cap applied to the architect invocation
// when generating a nebula from a ticket. The ticket-driven entry point does
// not (yet) thread a configurable budget; callers needing finer control can
// adjust the manifest after generation.
const defaultTicketBudgetUSD = 50.0

// maxTicketNebulaName bounds the generated nebula directory name length. The
// full name (prefix, reference, and title slug) is truncated to this many
// characters.
const maxTicketNebulaName = 40

//go:embed prompt_ticket.tmpl
var ticketPromptSource string

// ticketPromptTmpl is parsed once at package initialization so the template is
// not re-parsed on every RenderTicketPrompt call. The join function is bound to
// strings.Join for rendering label lists.
var ticketPromptTmpl = template.Must(
	template.New("ticket-prompt").
		Funcs(template.FuncMap{"join": strings.Join}).
		Parse(ticketPromptSource),
)

// NebulaInfo identifies a nebula generated from a ticket and written to disk.
type NebulaInfo struct {
	Name   string          // generated directory name (e.g. "nebula-42-fix-truncate")
	Path   string          // path to the written nebula directory
	Result *GenerateResult // full generation result for callers needing detail
}

// FromTicket runs the architect against a ticket context. It renders the ticket
// into the architect's standard prompt format (see prompt_ticket.tmpl), invokes
// the LLM through the shared generation back-half, and writes the resulting
// nebula directory under outDir.
//
// The directory name is derived from the ticket (see slugifyTicket); the
// returned NebulaInfo carries that name and the on-disk path. The generated
// manifest records the ticket's SourceName and SourceID so provenance is
// preserved. Collision handling against existing directories is the caller's
// responsibility: WriteNebula refuses to overwrite, so a re-pulled ticket
// surfaces ErrDirExists.
func FromTicket(ctx context.Context, invoker agent.Invoker, t *integrations.Ticket, outDir string) (*NebulaInfo, error) {
	if t == nil {
		return nil, fmt.Errorf("cannot generate nebula from nil ticket")
	}
	if outDir == "" {
		return nil, fmt.Errorf("from ticket requires a non-empty output directory")
	}
	return FromTicketInto(ctx, invoker, t, filepath.Join(outDir, slugifyTicket(t)))
}

// FromTicketInto is FromTicket with caller-chosen placement: it writes the
// generated nebula into targetDir exactly, using filepath.Base(targetDir) as
// the nebula name. The caller owns name selection and collision resolution
// (FromTicket derives the name and joins it onto a parent dir; the `nebula new`
// command needs to honor a --name override and append numeric suffixes to avoid
// clobbering an existing draft, so it calls this variant instead).
//
// WriteNebula refuses to overwrite, so targetDir must not already exist.
func FromTicketInto(ctx context.Context, invoker agent.Invoker, t *integrations.Ticket, targetDir string) (*NebulaInfo, error) {
	if t == nil {
		return nil, fmt.Errorf("cannot generate nebula from nil ticket")
	}
	if targetDir == "" {
		return nil, fmt.Errorf("from ticket requires a non-empty target directory")
	}

	prompt, err := RenderTicketPrompt(t)
	if err != nil {
		return nil, fmt.Errorf("rendering ticket prompt: %w", err)
	}

	name := filepath.Base(targetDir)

	// The UserPrompt here feeds only the manifest description and goals; the
	// actual architect prompt is the rendered ticket above.
	req := GenerateRequest{
		UserPrompt:   t.Title,
		NebulaName:   name,
		OutputDir:    targetDir,
		MaxBudgetUSD: defaultTicketBudgetUSD,
	}

	manifest := buildManifest(req)
	manifest.Nebula.SourceName = t.SourceName
	manifest.Nebula.SourceID = t.SourceID

	result, err := runGenerate(ctx, invoker, req, manifest, prompt)
	if err != nil {
		return nil, err
	}

	// Mirror provenance onto the in-memory nebula for callers that inspect it
	// without re-reading the written manifest.
	if result.Nebula != nil {
		result.Nebula.SourceName = t.SourceName
		result.Nebula.SourceID = t.SourceID
	}

	if err := WriteNebula(result, targetDir, WriteOptions{}); err != nil {
		return nil, fmt.Errorf("writing nebula %q: %w", name, err)
	}

	return &NebulaInfo{Name: name, Path: targetDir, Result: result}, nil
}

// SlugifyTicket returns the default nebula directory name for a ticket
// ("nebula-<number>-<title-slug>"). The `nebula new` command uses it to derive
// a base name before resolving on-disk collisions.
func SlugifyTicket(t *integrations.Ticket) string {
	return slugifyTicket(t)
}

// RenderTicketPrompt produces the architect's user prompt for a ticket-driven
// nebula. It is kept separate from the generation flow so it can be unit-tested
// without any LLM dependency.
func RenderTicketPrompt(t *integrations.Ticket) (string, error) {
	if t == nil {
		return "", fmt.Errorf("cannot render prompt for nil ticket")
	}
	var b strings.Builder
	if err := ticketPromptTmpl.Execute(&b, t); err != nil {
		return "", fmt.Errorf("executing ticket prompt template: %w", err)
	}
	return b.String(), nil
}

// slugifyTicket derives a nebula directory name from a ticket following the
// spec convention:
//
//   - "nebula-<number>-<slug-of-title>" for tickets with a numeric Number
//   - "nebula-<slugified-source-id>-<slug-of-title>" for tickets without one
//
// The result is lowercase ASCII, hyphen-separated, and capped at
// maxTicketNebulaName characters. The caller resolves on-disk collisions.
func slugifyTicket(t *integrations.Ticket) string {
	ref := strconv.Itoa(t.Number)
	if t.Number <= 0 {
		ref = slugify(t.SourceID)
	}
	if ref == "" {
		ref = "ticket"
	}

	name := "nebula-" + ref
	if titleSlug := slugify(t.Title); titleSlug != "" {
		name += "-" + titleSlug
	}

	if len(name) > maxTicketNebulaName {
		name = strings.TrimRight(name[:maxTicketNebulaName], "-")
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

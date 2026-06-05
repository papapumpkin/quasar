// Package integrations defines the forge-agnostic boundary between Quasar and
// external work-tracking systems (GitHub Issues, Jira, Linear, etc.).
//
// The package owns four things:
//
//   - The read-side TicketSource interface and the Ticket DTO that adapters
//     return.
//   - The write-side Forge interface stub (Name() only in this phase; the full
//     PR/comment/status surface lands in a later nebula).
//   - A Registry that maps an adapter name to its constructor for both
//     interface families.
//   - A Docker-friendly credential resolver (SecretSpec / ResolveSecret).
//
// Concrete adapters live in sub-packages (e.g. internal/integrations/github)
// and register themselves with Default() from their package init().
package integrations

import (
	"context"
	"time"
)

// Ticket is the source-agnostic representation of a unit of work pulled from
// an external tracker. Adapters convert their native shape (GitHub Issue,
// Jira Issue, Linear Issue, etc.) into this DTO at the integration boundary.
type Ticket struct {
	SourceName     string            // adapter name, e.g. "github"
	SourceID       string            // adapter-canonical id, e.g. "papapumpkin/quasar#42"
	Number         int               // human-facing number when applicable; 0 if N/A
	Title          string            //
	Body           string            // markdown
	State          string            // "open" | "closed" | adapter-specific
	Labels         []string          //
	Assignee       string            //
	URL            string            // browser-clickable
	Comments       []Comment         // chronological
	LinkedWork     []string          // PR/MR/cross-ticket refs (URLs or source-ids)
	SourceMetadata map[string]string // adapter-specific extras (sprint, milestone, etc.)
}

// Comment is a single chronological comment on a Ticket.
type Comment struct {
	Author    string
	Body      string
	CreatedAt time.Time
}

// TicketSource is the read-side integration with an external work-tracking
// system. Implementations are registered in a Registry via init() so they can
// be looked up by name at runtime.
//
// Implementations MUST be safe for concurrent use.
type TicketSource interface {
	// Name returns the adapter name (e.g. "github", "jira"). Used as the
	// registry key and the SourceName field on Tickets it returns.
	Name() string

	// Fetch retrieves a single ticket plus its comments and any cross-refs
	// the adapter can resolve cheaply. Implementations should NOT fetch
	// transitively reachable work (e.g. don't follow linked PRs).
	//
	// The sourceID format is adapter-specific (see Ticket.SourceID).
	Fetch(ctx context.Context, sourceID string) (*Ticket, error)
}

// Forge is the write-side integration with a Git forge (PR/MR creation,
// comment polling, status sync). This interface is reserved here so the
// .quasar.yaml [forge.*] schema and the registry pattern are uniform across
// integration kinds, but its surface is intentionally minimal in this nebula.
// The full methods land in a later nebula (master-review-pr-loop).
type Forge interface {
	// Name returns the forge adapter name (e.g. "github").
	Name() string
}

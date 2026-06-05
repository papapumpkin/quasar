// Package sensors defines the poll-driven boundary between Quasar and external
// work-tracking systems (GitHub Issues, Jira, Linear, etc.).
//
// A sensor runs on a persistent service: a scheduler goroutine periodically
// calls Poll, and each returned Event is rendered (via SeedNebula) into a seed
// nebula — a draft row in the nebulas table that the architect constellation
// later refines into executable phases.
//
// The package owns four things:
//
//   - The poll-driven Sensor interface and the Event / SeedNebulaContent values
//     adapters produce.
//   - The Ticket DTO, retained as the rendered source shape that flows into the
//     architect prompt (see internal/nebula/prompt_nebula.go).
//   - The write-side Forge interface stub (Name() only in this phase; the full
//     PR/comment/status surface lands in a later nebula).
//   - A Registry mapping a sensor/forge name to its constructor, plus a
//     Docker-friendly credential resolver (SecretSpec / ResolveSecret).
//
// Concrete adapters live in sub-packages (e.g. internal/sensors/github) and
// register themselves with Default() from their package init().
package sensors

import (
	"context"
	"encoding/json"
	"time"
)

// Ticket is the source-agnostic representation of a unit of work pulled from an
// external tracker. Adapters convert their native shape (GitHub Issue, Jira
// Issue, Linear Issue, etc.) into this DTO when rendering source context for
// the architect prompt.
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

// Event is a single unit of work a Sensor observed during Poll. ExternalID is
// the sensor-defined identity (unique per source) the runtime uses to
// deduplicate; Raw carries the adapter-internal payload that SeedNebula later
// renders into seed-nebula content.
type Event struct {
	ExternalID string         // sensor-defined, unique per source
	Timestamp  time.Time      //
	Raw        map[string]any // adapter-internal payload
}

// SeedNebulaContent is the structured content a Sensor produces for one Event.
// The runtime writes it into the nebulas table as a draft row; the sensor does
// not touch the database itself.
type SeedNebulaContent struct {
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

// Sensor is a poll-driven integration with an external work-tracking system.
// Implementations live in their own subpackage of internal/sensors/ and
// register a constructor with the package-level registry via init().
//
// The sensor's job is to produce seed nebulas. A seed nebula is a row in the
// nebulas table with status='draft', a populated source block, basic context
// (goals + constraints derived from the source), but NO phases. The architect
// constellation refines seed nebulas into executable plans.
//
// Implementations MUST be safe for concurrent use; the supervisor runs one
// scheduler goroutine per sensor instance.
type Sensor interface {
	// Name returns the sensor type's name (e.g. "github_issues",
	// "jira_issues"). Used as the registry key.
	Name() string

	// Configure parses the config block from the sensor's TOML instance file.
	// Returns a typed error so `quasar lint` can surface misconfiguration
	// before the supervisor boots.
	Configure(raw map[string]any, secrets SecretResolver) error

	// Poll returns events that occurred since the cursor. The runtime persists
	// newCursor before processing the events, so sensors do not have to manage
	// cursor durability themselves. Cursor is an opaque JSON value the sensor
	// defines for itself; an empty cursor means "first poll".
	Poll(ctx context.Context, cursor json.RawMessage) (events []Event, newCursor json.RawMessage, err error)

	// SeedNebula renders a single event into the seed nebula content that will
	// be inserted into SQLite. The runtime handles the DB write; the sensor
	// just produces the structured content.
	SeedNebula(event Event) (*SeedNebulaContent, error)
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

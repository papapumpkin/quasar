package beads

import "context"

// BeadsClient defines the interface for interacting with beads.
// *Client satisfies this interface.
type BeadsClient interface {
	Create(ctx context.Context, title string, opts CreateOpts) (string, error)
	Show(ctx context.Context, id string) (*Bead, error)
	Update(ctx context.Context, id string, opts UpdateOpts) error
	Close(ctx context.Context, id string, reason string) error
	AddComment(ctx context.Context, id string, body string) error
	Validate() error
}

// Bead represents a single bead issue returned from the beads CLI.
type Bead struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Priority    int      `json:"priority"`
	IssueType   string   `json:"issue_type"`
	Assignee    string   `json:"assignee"`
	Labels      []string `json:"labels"`
	ParentID    string   `json:"parent_id,omitempty"`
}

// CreateOpts holds optional parameters for creating a new bead.
type CreateOpts struct {
	Description string
	Type        string
	Labels      []string
	Parent      string
	Assignee    string
	Priority    string
}

// UpdateOpts holds optional parameters for updating an existing bead.
type UpdateOpts struct {
	Status   string
	Assignee string
}

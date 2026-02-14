package beads

// BeadsClient defines the interface for interacting with beads.
// *Client satisfies this interface.
type BeadsClient interface {
	Create(title string, opts CreateOpts) (string, error)
	Show(id string) (*Bead, error)
	Update(id string, opts UpdateOpts) error
	Close(id string, reason string) error
	AddComment(id string, body string) error
	Validate() error
}

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

type CreateOpts struct {
	Description string
	Type        string
	Labels      []string
	Parent      string
	Assignee    string
	Priority    string
}

type UpdateOpts struct {
	Status   string
	Assignee string
}

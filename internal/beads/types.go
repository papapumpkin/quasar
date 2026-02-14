package beads

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

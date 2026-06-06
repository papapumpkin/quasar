package fleet

import (
	"strings"
	"time"
)

// Filter is a parsed fleet filter. Empty fields are unconstrained; constraints
// are AND'd together when applied.
type Filter struct {
	Repo  string        // substring match on repo display name / path
	State string        // exact match on card status / run state
	Since time.Duration // max age; zero means unconstrained
	Text  string        // substring match on card title (case-insensitive)
}

// Empty reports whether the filter constrains nothing.
func (f Filter) Empty() bool {
	return f.Repo == "" && f.State == "" && f.Since == 0 && f.Text == ""
}

// ParseFilter parses a filter expression. Tokens of the form key:value are
// recognized for repo, state, and since (a Go duration); all other whitespace-
// separated tokens are joined into a free-text substring match.
func ParseFilter(raw string) Filter {
	var f Filter
	var text []string
	for _, tok := range strings.Fields(raw) {
		key, val, ok := strings.Cut(tok, ":")
		if !ok {
			text = append(text, tok)
			continue
		}
		switch strings.ToLower(key) {
		case "repo":
			f.Repo = val
		case "state", "status":
			f.State = val
		case "since":
			if d, err := time.ParseDuration(val); err == nil {
				f.Since = d
			}
		default:
			text = append(text, tok)
		}
	}
	f.Text = strings.Join(text, " ")
	return f
}

// Apply returns a copy of the fleet narrowed by the filter. Repo lanes with no
// matching cards in any lane are dropped when the filter is non-empty.
func (f Filter) Apply(in Fleet) Fleet {
	if f.Empty() {
		return in
	}
	var out Fleet
	for _, lane := range in.Repos {
		if f.Repo != "" && !containsFold(lane.DisplayName, f.Repo) && !containsFold(lane.Path, f.Repo) {
			continue
		}
		lane.AwaitingApproval = f.filterNebulas(lane.AwaitingApproval)
		lane.Recent = f.filterNebulas(lane.Recent)
		lane.InFlight = f.filterRuns(lane.InFlight)
		if len(lane.AwaitingApproval) == 0 && len(lane.Recent) == 0 && len(lane.InFlight) == 0 {
			continue
		}
		out.Repos = append(out.Repos, lane)
	}
	return out
}

// filterNebulas keeps the nebula cards matching the state/since/text constraints.
func (f Filter) filterNebulas(cards []NebulaCard) []NebulaCard {
	var out []NebulaCard
	for _, c := range cards {
		if f.State != "" && c.Status != f.State {
			continue
		}
		if f.Since != 0 && c.Age > f.Since {
			continue
		}
		if f.Text != "" && !containsFold(c.Title, f.Text) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// filterRuns keeps the run cards matching the state/text constraints. Runs have
// no age, so the since constraint excludes them.
func (f Filter) filterRuns(runs []RunCard) []RunCard {
	if f.Since != 0 {
		return nil
	}
	var out []RunCard
	for _, r := range runs {
		if f.State != "" && r.State != f.State {
			continue
		}
		if f.Text != "" && !containsFold(r.NebulaTitle, f.Text) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// containsFold reports whether s contains substr, case-insensitively.
func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// Package discussions is the worked-example sensor from docs/sensor-authoring.md.
// It polls a GitHub repository's Discussions via the gh CLI and seeds one nebula
// per new discussion. It is a complete, compiling reference, not a production
// adapter (it omits the typed errors and filter logic the github_issues sensor
// carries).
package discussions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/papapumpkin/quasar/internal/sensors"
)

// init registers the sensor under the type name a repo's sensor TOML selects
// with `type = "github_discussions"`. The constructor returns (Sensor, error)
// to match sensors.SensorConstructor.
func init() {
	sensors.Default().RegisterSensor("github_discussions", func() (sensors.Sensor, error) {
		return New(), nil
	})
}

// Source polls GitHub Discussions for one repository.
type Source struct {
	repo  string
	token string
}

// New constructs an unconfigured Source.
func New() *Source { return &Source{} }

// Compile-time check that Source satisfies the sensor contract.
var _ sensors.Sensor = (*Source)(nil)

// Name returns the registry key.
func (s *Source) Name() string { return "github_discussions" }

// Configure reads repo from the instance [config] block and resolves the gh
// token via the file > env precedence. An empty token defers to gh's own auth.
func (s *Source) Configure(raw map[string]any, secrets sensors.SecretResolver) error {
	repo, _ := raw["repo"].(string)
	if repo == "" {
		return fmt.Errorf("github_discussions: repo is required")
	}
	token, err := secrets.Resolve(sensors.SecretSpec{
		Env:  asString(raw["token_env"]),
		File: asString(raw["token_file"]),
	})
	if err != nil {
		return fmt.Errorf("github_discussions: resolve token: %w", err)
	}
	s.repo, s.token = repo, token
	return nil
}

// cursor is the highest discussion number seen so far — a dense monotonic id, so
// the numeric-counter cursor pattern applies.
type cursor struct {
	LastNumber int `json:"last_number"`
}

// discussion is the subset of the gh JSON this sensor reads.
type discussion struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	URL    string `json:"url"`
}

// Poll lists discussions and emits one Event per discussion newer than the
// cursor, advancing newCursor to the highest number seen.
func (s *Source) Poll(ctx context.Context, raw json.RawMessage) ([]sensors.Event, json.RawMessage, error) {
	var cur cursor
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cur); err != nil {
			return nil, raw, fmt.Errorf("github_discussions: decode cursor: %w", err)
		}
	}

	out, err := s.gh(ctx, "api", fmt.Sprintf("repos/%s/discussions", s.repo))
	if err != nil {
		return nil, raw, err
	}
	var list []discussion
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, raw, fmt.Errorf("github_discussions: parse: %w", err)
	}

	highest := cur.LastNumber
	var events []sensors.Event
	for _, d := range list {
		if d.Number <= cur.LastNumber {
			continue // already seeded on an earlier poll
		}
		if d.Number > highest {
			highest = d.Number
		}
		events = append(events, sensors.Event{
			// Stable + unique within (repo, sensor): the dedup key.
			ExternalID: fmt.Sprintf("%s#%d", s.repo, d.Number),
			Raw:        map[string]any{"title": d.Title, "body": d.Body, "url": d.URL},
		})
	}

	next, err := json.Marshal(cursor{LastNumber: highest})
	if err != nil {
		return nil, raw, fmt.Errorf("github_discussions: encode cursor: %w", err)
	}
	return events, next, nil
}

// SeedNebula renders one discussion Event into seed-nebula content.
func (s *Source) SeedNebula(ev sensors.Event) (*sensors.SeedNebulaContent, error) {
	title := asString(ev.Raw["title"])
	return &sensors.SeedNebulaContent{
		Name:        title,
		Description: asString(ev.Raw["body"]),
		SourceName:  "github",
		SourceID:    ev.ExternalID,
		SourceURL:   asString(ev.Raw["url"]),
		Goals:       []string{title},
	}, nil
}

// gh shells out to the gh binary, injecting the resolved token when present.
func (s *Source) gh(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	if s.token != "" {
		cmd.Env = append(cmd.Environ(), "GH_TOKEN="+s.token)
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("github_discussions: gh %v: %w", args, err)
	}
	return stdout.Bytes(), nil
}

// asString returns v as a string, or "" when it is absent or not a string.
func asString(v any) string {
	s, _ := v.(string)
	return s
}

package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/fabric"
)

func init() {
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Show fabric changes since a timestamp",
		Long: `Shows entanglements and discoveries created after the given timestamp.
Useful for agents to see what changed in the fabric while they were working.

Timestamp format: RFC3339 (e.g. 2024-01-15T10:30:00Z) or DateTime (e.g. 2024-01-15 10:30:00).`,
		RunE: runFabricDiff,
	}
	cmd.Flags().String("since", "", "show changes after this timestamp (required)")
	_ = cmd.MarkFlagRequired("since")
	fabricCmd.AddCommand(cmd)
}

func runFabricDiff(cmd *cobra.Command, _ []string) error {
	sinceStr, _ := cmd.Flags().GetString("since")

	since, err := parseSinceTimestamp(sinceStr)
	if err != nil {
		return fmt.Errorf("fabric diff: invalid --since value: %w", err)
	}

	f, err := openFabric(cmd)
	if err != nil {
		return err
	}
	defer f.Close()

	ctx := cmd.Context()

	entanglements, err := f.AllEntanglements(ctx)
	if err != nil {
		return fmt.Errorf("fabric diff: entanglements: %w", err)
	}

	discoveries, err := f.AllDiscoveries(ctx)
	if err != nil {
		return fmt.Errorf("fabric diff: discoveries: %w", err)
	}

	// Filter to items created after the since timestamp.
	var newEntanglements []fabric.Entanglement
	for _, e := range entanglements {
		if e.CreatedAt.After(since) {
			newEntanglements = append(newEntanglements, e)
		}
	}

	var newDiscoveries []fabric.Discovery
	for _, d := range discoveries {
		if d.CreatedAt.After(since) {
			newDiscoveries = append(newDiscoveries, d)
		}
	}

	fmt.Fprint(cmd.OutOrStdout(), renderDiff(since, newEntanglements, newDiscoveries))
	return nil
}

// renderDiff formats the fabric changes since a given timestamp.
func renderDiff(since time.Time, entanglements []fabric.Entanglement, discoveries []fabric.Discovery) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Fabric Changes Since %s\n", since.Format(time.RFC3339))

	b.WriteString("\n### New Entanglements\n")
	if len(entanglements) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, e := range entanglements {
			display := e.Name
			if e.Signature != "" {
				display = e.Signature
			}
			fmt.Fprintf(&b, "- [%s] %s %s (producer: %s, pkg: %s)\n",
				e.Status, e.Kind, display, e.Producer, e.Package)
		}
	}

	b.WriteString("\n### New Discoveries\n")
	if len(discoveries) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, d := range discoveries {
			resolved := ""
			if d.Resolved {
				resolved = " [resolved]"
			}
			if d.Affects != "" {
				fmt.Fprintf(&b, "- #%d [%s] %s (from: %s, affects: %s)%s\n",
					d.ID, d.Kind, d.Detail, d.SourceTask, d.Affects, resolved)
			} else {
				fmt.Fprintf(&b, "- #%d [%s] %s (from: %s)%s\n",
					d.ID, d.Kind, d.Detail, d.SourceTask, resolved)
			}
		}
	}

	return b.String()
}

// parseSinceTimestamp attempts to parse a timestamp in RFC3339 or DateTime format.
func parseSinceTimestamp(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		time.DateTime,
	}
	for _, layout := range formats {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("expected RFC3339 (2024-01-15T10:30:00Z) or DateTime (2024-01-15 10:30:00) format, got %q", s)
}

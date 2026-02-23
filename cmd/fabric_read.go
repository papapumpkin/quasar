package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/fabric"
)

func init() {
	fabricCmd.AddCommand(&cobra.Command{
		Use:   "read",
		Short: "Output full fabric state as structured text",
		Long: `Reads the entire fabric state and outputs it as plain text suitable for
agent consumption. Includes entanglements, file claims, phase states, beads,
and unresolved discoveries.`,
		RunE: runFabricRead,
	})
}

func runFabricRead(cmd *cobra.Command, _ []string) error {
	f, err := openFabric(cmd)
	if err != nil {
		return err
	}
	defer f.Close()

	ctx := cmd.Context()

	entanglements, err := f.AllEntanglements(ctx)
	if err != nil {
		return fmt.Errorf("fabric read: entanglements: %w", err)
	}

	discoveries, err := f.AllDiscoveries(ctx)
	if err != nil {
		return fmt.Errorf("fabric read: discoveries: %w", err)
	}

	unresolved, err := f.UnresolvedDiscoveries(ctx)
	if err != nil {
		return fmt.Errorf("fabric read: unresolved discoveries: %w", err)
	}

	// Build snapshot for rendering.
	snap := fabric.FabricSnapshot{
		Entanglements:         entanglements,
		UnresolvedDiscoveries: unresolved,
	}

	// Render core snapshot.
	out := fabric.RenderSnapshot(snap)

	// Append all discoveries section (including resolved).
	var b strings.Builder
	b.WriteString(out)

	b.WriteString("\n### All Discoveries\n")
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

	fmt.Fprint(cmd.OutOrStdout(), b.String())
	return nil
}

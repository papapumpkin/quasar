package cmd

import (
	"fmt"
	"sort"
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

	claims, err := f.AllClaims(ctx)
	if err != nil {
		return fmt.Errorf("fabric read: claims: %w", err)
	}

	phaseStates, err := f.AllPhaseStates(ctx)
	if err != nil {
		return fmt.Errorf("fabric read: phase states: %w", err)
	}

	// Build file claims map and categorize phases by state.
	fileClaims := make(map[string]string, len(claims))
	for _, c := range claims {
		fileClaims[c.Filepath] = c.OwnerTask
	}

	var completed, inProgress []string
	for id, state := range phaseStates {
		switch state {
		case fabric.StateDone:
			completed = append(completed, id)
		case fabric.StateRunning, fabric.StateScanning:
			inProgress = append(inProgress, id)
		}
	}
	sort.Strings(completed)
	sort.Strings(inProgress)

	// Build snapshot for rendering.
	snap := fabric.Snapshot{
		Entanglements:         entanglements,
		FileClaims:            fileClaims,
		Completed:             completed,
		InProgress:            inProgress,
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

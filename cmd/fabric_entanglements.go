package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/fabric"
)

func init() {
	cmd := &cobra.Command{
		Use:   "entanglements",
		Short: "List entanglements, optionally filtered by task",
		Long: `Lists entanglements from the fabric. By default shows all entanglements.
Use --task to filter to entanglements produced by a specific task.

Output is grouped by producer and shows status (pending/fulfilled/disputed).`,
		RunE: runFabricEntanglements,
	}
	fabricCmd.AddCommand(cmd)
}

func runFabricEntanglements(cmd *cobra.Command, _ []string) error {
	f, err := openFabric(cmd)
	if err != nil {
		return err
	}
	defer f.Close()

	ctx := cmd.Context()
	taskID := fabricTaskID()

	var entanglements []fabric.Entanglement
	if taskID != "" {
		entanglements, err = f.EntanglementsFor(ctx, taskID)
	} else {
		entanglements, err = f.AllEntanglements(ctx)
	}
	if err != nil {
		return fmt.Errorf("fabric entanglements: %w", err)
	}

	fmt.Fprint(cmd.OutOrStdout(), renderEntanglements(entanglements))
	return nil
}

// renderEntanglements formats entanglements grouped by producer.
func renderEntanglements(entanglements []fabric.Entanglement) string {
	if len(entanglements) == 0 {
		return "No entanglements found.\n"
	}

	// Group by producer.
	grouped := make(map[string][]fabric.Entanglement)
	for _, e := range entanglements {
		grouped[e.Producer] = append(grouped[e.Producer], e)
	}

	producers := make([]string, 0, len(grouped))
	for p := range grouped {
		producers = append(producers, p)
	}
	sort.Strings(producers)

	var b strings.Builder
	b.WriteString("## Entanglements\n")
	for _, producer := range producers {
		ents := grouped[producer]
		fmt.Fprintf(&b, "\n### Producer: %s\n", producer)
		for _, e := range ents {
			display := e.Name
			if e.Signature != "" {
				display = e.Signature
			}
			fmt.Fprintf(&b, "- [%s] %s %s (pkg: %s, status: %s)\n",
				e.Status, e.Kind, display, e.Package, e.Status)
		}
	}
	return b.String()
}

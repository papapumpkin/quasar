package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/telemetry"
)

var telemetryCmd = &cobra.Command{
	Use:   "telemetry",
	Short: "View JSONL telemetry events for a nebula epoch",
	Long: `Reads and formats the JSONL telemetry file for the current or specified epoch.

Without --epoch, discovers the most recent telemetry file.
With --follow (-f), watches the file for new events (like tail -f).`,
	RunE: runTelemetry,
}

func init() {
	telemetryCmd.Flags().String("epoch", "", "epoch ID to view (default: most recent)")
	telemetryCmd.Flags().BoolP("follow", "f", false, "follow the file for new events")
	rootCmd.AddCommand(telemetryCmd)
}

func runTelemetry(cmd *cobra.Command, _ []string) error {
	epochID, _ := cmd.Flags().GetString("epoch")
	follow, _ := cmd.Flags().GetBool("follow")

	path, err := resolveTelemetryPath(epochID)
	if err != nil {
		return err
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("telemetry: open %s: %w", path, err)
	}
	defer f.Close()

	// Print all existing events.
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		printEvent(cmd.OutOrStdout(), line)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("telemetry: read %s: %w", path, err)
	}

	if !follow {
		return nil
	}

	return tailFollow(cmd.OutOrStdout(), f, path)
}

// tailFollow watches the file for new data using fsnotify and prints new events.
func tailFollow(w io.Writer, f *os.File, path string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("telemetry: create watcher: %w", err)
	}
	defer watcher.Close()

	if err := watcher.Add(path); err != nil {
		return fmt.Errorf("telemetry: watch %s: %w", path, err)
	}

	reader := bufio.NewReader(f)
	for event := range watcher.Events {
		if event.Op&fsnotify.Write == 0 {
			continue
		}
		// Read all new lines available.
		for {
			line, err := reader.ReadString('\n')
			line = strings.TrimSpace(line)
			if line != "" {
				printEvent(w, line)
			}
			if err != nil {
				break
			}
		}
	}
	return nil
}

// printEvent decodes a JSONL line and prints a human-readable representation.
func printEvent(w io.Writer, line string) {
	var evt telemetry.Event
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		fmt.Fprintf(w, "??? %s\n", line)
		return
	}

	ts := evt.Timestamp.Format(time.TimeOnly)
	var parts []string
	parts = append(parts, fmt.Sprintf("[%s]", ts))
	parts = append(parts, evt.Kind)

	if evt.EpochID != "" {
		parts = append(parts, fmt.Sprintf("epoch=%s", evt.EpochID))
	}
	if evt.TaskID != "" {
		parts = append(parts, fmt.Sprintf("task=%s", evt.TaskID))
	}
	if evt.Data != nil {
		if m, ok := evt.Data.(map[string]any); ok {
			parts = append(parts, formatDataMap(m))
		} else {
			data, _ := json.Marshal(evt.Data)
			parts = append(parts, string(data))
		}
	}

	fmt.Fprintln(w, strings.Join(parts, " "))
}

// formatDataMap formats a data map as key=value pairs sorted by key.
func formatDataMap(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%s=%v", k, m[k])
	}
	return b.String()
}

// resolveTelemetryPath finds the JSONL file for the given epoch, or discovers
// the most recent one if epochID is empty.
func resolveTelemetryPath(epochID string) (string, error) {
	dir := filepath.Join(".quasar", "telemetry")

	if epochID != "" {
		path := filepath.Join(dir, epochID+".jsonl")
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("telemetry: no file for epoch %q: %w", epochID, err)
		}
		return path, nil
	}

	// Discover the most recent telemetry file.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("telemetry: cannot read %s: %w", dir, err)
	}

	var jsonlFiles []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			jsonlFiles = append(jsonlFiles, e)
		}
	}
	if len(jsonlFiles) == 0 {
		return "", fmt.Errorf("telemetry: no JSONL files in %s", dir)
	}

	// Sort by modification time, most recent last.
	sort.Slice(jsonlFiles, func(i, j int) bool {
		fi, _ := jsonlFiles[i].Info()
		fj, _ := jsonlFiles[j].Info()
		return fi.ModTime().Before(fj.ModTime())
	})

	return filepath.Join(dir, jsonlFiles[len(jsonlFiles)-1].Name()), nil
}

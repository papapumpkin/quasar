+++
id = "cli-commands"
title = "CLI commands: quasar canvas, --resume, --web, --list"
type = "feature"
priority = 2
depends_on = ["cli-conversation", "session-persistence", "web-chat-ui"]
scope = ["cmd/canvas.go"]
+++

## Problem

The canvas conversation loop (phase 3), session persistence (phase 5), and web chat UI (phase 6) exist as library code but have no CLI entry point. Users need a `quasar canvas` command that ties everything together with flags for creating new sessions, resuming old ones, listing sessions, and choosing between CLI REPL and web UI.

## Solution

### Command registration

```go
// cmd/canvas.go

package cmd

import (
    "fmt"
    "os"

    "github.com/aaronsalm/quasar/internal/canvas"
    "github.com/aaronsalm/quasar/internal/claude"
    "github.com/aaronsalm/quasar/internal/config"
    "github.com/spf13/cobra"
)

var canvasCmd = &cobra.Command{
    Use:   "canvas [name]",
    Short: "Start a conversational nebula authoring session",
    Long: `Canvas opens an interactive session where you describe your goals
in natural language and an AI architect agent organizes them into
well-structured nebula manifests and phase files.

Without arguments, starts a new unnamed session. With a name argument,
creates a named session (the nebula will be generated as .nebulas/<name>/).

Use --resume to continue a previous session, --list to browse sessions,
or --web to use the browser-based chat UI.`,
    Args: cobra.MaximumNArgs(1),
    RunE: runCanvas,
}

func init() {
    rootCmd.AddCommand(canvasCmd)

    canvasCmd.Flags().String("resume", "", "Resume a previous session by ID")
    canvasCmd.Flags().Bool("list", false, "List all saved canvas sessions")
    canvasCmd.Flags().Bool("web", false, "Open web chat UI instead of CLI REPL")
    canvasCmd.Flags().Int("web-port", 0, "Port for web chat UI (0 = auto-assign)")
    canvasCmd.Flags().Bool("delete", false, "Delete a session (use with --resume)")
    canvasCmd.Flags().String("model", "", "Model override for the architect agent")
}
```

### runCanvas

```go
func runCanvas(cmd *cobra.Command, args []string) error {
    cfg, err := config.Load()
    if err != nil {
        return fmt.Errorf("load config: %w", err)
    }

    store, err := canvas.NewSessionStore(".quasar/canvas")
    if err != nil {
        return fmt.Errorf("init session store: %w", err)
    }

    // --list: show all sessions and exit.
    listMode, _ := cmd.Flags().GetBool("list")
    if listMode {
        return listSessions(store)
    }

    // --resume + --delete: delete a session.
    resumeID, _ := cmd.Flags().GetString("resume")
    deleteMode, _ := cmd.Flags().GetBool("delete")
    if deleteMode && resumeID != "" {
        return store.Delete(resumeID)
    }

    // --web: launch web chat UI.
    useWeb, _ := cmd.Flags().GetBool("web")
    if useWeb {
        return runCanvasWeb(cmd, store, resumeID, cfg)
    }

    // Create or resume session.
    var session *canvas.Session
    if resumeID != "" {
        session, err = store.Load(resumeID)
        if err != nil {
            return fmt.Errorf("resume session: %w", err)
        }
        fmt.Fprintf(os.Stderr, "Resuming session %q (%d turns, %d phases)\n",
            session.Name, len(session.Turns), len(session.DraftPhases))
    } else {
        name := ""
        if len(args) > 0 {
            name = args[0]
        }
        session = canvas.NewSession(name)
    }

    // Create architect agent.
    model, _ := cmd.Flags().GetString("model")
    if model == "" {
        model = cfg.Model
    }
    invoker := claude.NewInvoker(cfg.ClaudePath)
    architect := canvas.NewArchitect(invoker, model)

    // Run REPL.
    repl := canvas.NewREPL(session, architect, store)
    return repl.Run(cmd.Context())
}
```

### List sessions

```go
func listSessions(store *canvas.SessionStore) error {
    summaries, err := store.List()
    if err != nil {
        return err
    }
    if len(summaries) == 0 {
        fmt.Fprintln(os.Stderr, "No canvas sessions found.")
        return nil
    }
    fmt.Fprintln(os.Stderr, "Canvas sessions:")
    for _, s := range summaries {
        status := "draft"
        if s.Generated {
            status = "generated"
        }
        fmt.Fprintf(os.Stderr, "  %-12s  %-20s  %d turns  %d phases  [%s]  %s\n",
            s.ID[:8]+"...", s.Name, s.TurnCount, s.PhaseCount,
            status, s.UpdatedAt.Format("2006-01-02 15:04"))
    }
    return nil
}
```

### Web mode

```go
func runCanvasWeb(cmd *cobra.Command, store *canvas.SessionStore, resumeID string, cfg *config.Config) error {
    port, _ := cmd.Flags().GetInt("web-port")
    // Reuse the meridian web server with canvas routes.
    srv, err := web.NewServer(web.ServerConfig{
        Port:        port,
        CanvasStore: store,
        CanvasOnly:  true, // Only serve canvas routes.
    })
    if err != nil {
        return err
    }

    ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
    defer cancel()

    addr, err := srv.Start(ctx)
    if err != nil {
        return err
    }

    url := fmt.Sprintf("http://%s/canvas", addr)
    if resumeID != "" {
        url = fmt.Sprintf("http://%s/canvas/%s", addr, resumeID)
    }
    fmt.Fprintf(os.Stderr, "[canvas] web UI at %s\n", url)
    openBrowser(url)

    <-ctx.Done()
    srv.Wait()
    return nil
}
```

## Files

- `cmd/canvas.go` — `canvasCmd` Cobra command, `runCanvas`, `listSessions`, `runCanvasWeb`, flag registration

## Acceptance Criteria

- [ ] `quasar canvas` starts a new unnamed CLI REPL session
- [ ] `quasar canvas my-feature` starts a named session that will generate `.nebulas/my-feature/`
- [ ] `quasar canvas --resume <id>` loads and resumes an existing session
- [ ] `quasar canvas --list` displays all saved sessions with ID, name, turn/phase counts, status
- [ ] `quasar canvas --web` opens the browser-based canvas chat UI
- [ ] `quasar canvas --web --resume <id>` opens the web UI for an existing session
- [ ] `quasar canvas --delete --resume <id>` deletes a session
- [ ] `quasar canvas --model claude-sonnet-4-6` overrides the architect model
- [ ] `--web-port` controls the web server port (0 = auto-assign)
- [ ] CLI REPL auto-saves after every turn
- [ ] `go build ./...` compiles
- [ ] `go vet ./...` passes

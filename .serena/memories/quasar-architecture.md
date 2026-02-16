# Quasar Architecture

## Project Structure
```
cmd/          - Cobra CLI commands (root, run, validate, version)
internal/
  agent/      - Agent types (coder, reviewer) and default prompts
  beads/      - Beads CLI wrapper (Client struct, types)
  claude/     - Claude CLI invoker
  config/     - Viper-based config loading
  loop/       - Core coder-reviewer loop, state, errors
  ui/         - Stderr-based UI printer (ANSI colors)
```

## TUI
- `internal/tui/` — BubbleTea-based terminal UI (bridge pattern)
- Bridge: `UIBridge` implements `ui.UI` via `tea.Program.Send()`
- Loop needs zero changes — bridge converts imperative UI calls to messages
- `TUIGater` implements `nebula.Gater` via msg+channel pattern
- TTY auto-detection: `--auto` on TTY → TUI; piped → stderr printer
- `--no-tui` flag forces stderr output on both `run` and `nebula apply`
- `WorkerGroup.Logger` field controls worker stderr output (nil=os.Stderr, io.Discard in TUI mode)

## Conventions
- All UI output → stderr; stdout reserved for structured output
- Cobra + Viper for CLI; config via `.quasar.yaml` / env `QUASAR_*`
- No external test frameworks; stdlib `testing` only
- Error types as sentinel vars (`var ErrFoo = errors.New(...)`)
- beads.Client wraps CLI exec; Loop.Beads is `*beads.Client`

## Commands
- `go build -o quasar .` — build
- `go test ./internal/loop/...` — run tests
- `go vet ./...` — lint

# Quasar Architecture

## Project Structure
```
cmd/          - Cobra CLI commands (root, run, validate, version, fabric, discovery, bead, telemetry)
internal/
  agent/      - Agent types (coder, reviewer) and default prompts
  beads/      - Beads CLI wrapper (Client struct, types) — issue lifecycle
  claude/     - Claude CLI invoker
  config/     - Viper-based config loading
  fabric/     - Shared coordination substrate (SQLite WAL) — was internal/board
  filter/     - Pre-reviewer deterministic checks (build/vet/lint/test/claims)
  loop/       - Core coder-reviewer loop, state, errors
  nebula/     - Multi-task orchestration (parse, validate, plan, apply, workers)
  neutron/    - Epoch archival (standalone SQLite snapshots)
  telemetry/  - JSONL event stream for state transitions
  tui/        - BubbleTea cockpit TUI
  tycho/      - DAG scheduler (extracted from WorkerGroup)
  ui/         - Stderr-based UI printer (ANSI colors)
```

## Canonical Vocabulary
| Concept | Name | Package |
|---|---|---|
| Shared state DB | Fabric | internal/fabric |
| Interface agreements | Entanglement | internal/fabric |
| File ownership | Claim | internal/fabric |
| Agent-surfaced issues | Discovery | internal/fabric |
| Agent working memory | Bead | internal/fabric |
| Pre-review checks | Filter | internal/filter |
| DAG scheduler | Tycho | internal/tycho |
| Archived snapshot | Neutron | internal/neutron |
| Event stream | Telemetry | internal/telemetry |
| TUI dashboard | Cockpit | internal/tui |
| Human interrupt | Hail | type within discovery |
| Execution instance | Epoch | CLI/archival concept |

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

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

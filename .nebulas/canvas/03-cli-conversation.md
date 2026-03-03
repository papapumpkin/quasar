+++
id = "cli-conversation"
title = "Interactive CLI conversation loop for canvas sessions"
type = "feature"
priority = 2
depends_on = ["architect-agent"]
scope = ["internal/canvas/repl.go", "internal/canvas/repl_test.go"]
+++

## Problem

With the canvas types (phase 1) and architect agent (phase 2) in place, there is no interactive interface for developers to actually use the conversational authoring flow. The developer needs a CLI REPL where they can type natural language descriptions, see the architect's responses with questions and proposals, and watch draft phases accumulate as the conversation progresses.

Quasar's convention is that all human-readable output goes to stderr (via `ui.Printer`) and stdout is reserved for structured data. The canvas REPL must follow this pattern: prompts and architect responses go to stderr, while the final generated nebula path (if any) goes to stdout.

## Solution

Create `internal/canvas/repl.go` with an interactive REPL that drives the canvas conversation loop.

### REPL Structure

```go
// REPL drives an interactive canvas conversation session.
// It reads user input from stdin, invokes the architect agent,
// displays responses on stderr, and manages the session lifecycle.
type REPL struct {
    session  *Session
    invoker  agent.Invoker
    printer  *ui.Printer
    scanner  *bufio.Scanner
    workDir  string
    budget   float64
    model    string
}

// NewREPL creates a new canvas REPL with the given dependencies.
func NewREPL(session *Session, invoker agent.Invoker, printer *ui.Printer, workDir string, budget float64, model string) *REPL
```

### Main Loop

```go
// Run starts the interactive conversation loop. It blocks until the
// user exits (Ctrl+D, "exit", "quit") or the context is cancelled.
// Returns the final session state.
func (r *REPL) Run(ctx context.Context) (*Session, error)
```

The `Run` method implements this loop:

1. **Print welcome banner** — show the session name and brief instructions
2. **Read user input** — `bufio.Scanner` on `os.Stdin`, with a `canvas> ` prompt on stderr
3. **Handle special commands**:
   - `exit` or `quit` — end the session
   - `status` — show current draft phase table
   - `phases` — list draft phases with IDs, titles, and dependency edges
   - `generate` — trigger nebula file generation (delegates to phase 4)
   - `save` — persist session (delegates to phase 5)
   - `help` — show available commands
4. **Invoke architect** — call `BuildCanvasPrompt` with the user's message, then invoke via `agent.Invoker`
5. **Parse response** — use `ParseCanvasResponse` to separate text from structured updates
6. **Display response** — print the architect's conversational text to stderr with color coding
7. **Apply updates** — if the response contains a `DraftUpdate`, call `ApplyUpdate` and show a summary of changes
8. **Record turns** — add both the user turn and the architect turn to the session

### Color-Coded Output

Use `ui.Printer` for formatted output. The REPL uses distinct visual treatment for:

- **User input echo**: dimmed/gray prefix `you: ` (user already typed it, this is the transcript)
- **Architect responses**: cyan/blue prefix `architect: ` with the response body
- **Draft updates**: green prefix showing phases added or modified (e.g., `+ added phase "api-endpoints"`)
- **Status table**: tabular display of phase ID, title, priority, and dependencies
- **Errors**: red prefix via `printer.Error()`

### Status Display

```go
// printStatus renders the current draft nebula state as a table
// on stderr, showing phase IDs, titles, priorities, and dependency edges.
func (r *REPL) printStatus()
```

The status table format:

```
Draft: my-nebula (4 phases)
┌─────────────────┬──────────────────────────────────┬──────┬────────────────┐
│ ID              │ Title                            │ Pri  │ Depends On     │
├─────────────────┼──────────────────────────────────┼──────┼────────────────┤
│ api-types       │ Define API request/response types│  1   │                │
│ api-endpoints   │ Implement REST endpoints         │  2   │ api-types      │
│ api-middleware   │ Auth and logging middleware       │  2   │ api-types      │
│ api-tests       │ Integration test suite           │  3   │ api-endpoints  │
└─────────────────┴──────────────────────────────────┴──────┴────────────────┘
```

Use `strings.Builder` and `fmt.Fprintf` for table construction, following the project's preference for stdlib over external table-rendering libraries.

### Conversation Context Management

Each architect invocation includes the full conversation history (via `BuildCanvasPrompt`). For long conversations this could exceed context limits. The REPL should track the total prompt size and, if it grows too large, summarize older turns:

```go
// summarizeOlderTurns replaces the oldest turns in the session with
// a single summary turn, keeping the most recent N turns intact.
// This prevents context window overflow in long sessions.
func (r *REPL) summarizeOlderTurns(ctx context.Context, keepRecent int) error
```

### Error Handling

- If the architect invocation fails (network error, budget exceeded), print the error and continue the loop — don't crash the session.
- If `ParseCanvasResponse` fails on malformed output, show the raw response and log the parse error.
- On `context.Done()`, gracefully save the session state before exiting.

## Files

- `internal/canvas/repl.go` — `REPL` struct, `NewREPL`, `Run`, `printStatus`, `summarizeOlderTurns`, special command handlers
- `internal/canvas/repl_test.go` — tests with mock `agent.Invoker`: basic conversation flow, special command handling (`status`, `phases`, `generate`, `exit`), error recovery (failed invocation continues loop), context cancellation

## Acceptance Criteria

- [ ] `REPL` struct accepts `agent.Invoker`, `*ui.Printer`, and `*Session` via constructor injection
- [ ] `Run` reads from `bufio.Scanner` on stdin and writes prompts/responses to stderr via `ui.Printer`
- [ ] `exit` and `quit` commands gracefully end the loop and return the final session
- [ ] `status` command prints a formatted table of draft phases with ID, title, priority, and dependencies
- [ ] `phases` command lists phase IDs with their dependency edges
- [ ] `help` command lists all available special commands
- [ ] User turns and architect turns are both recorded in `session.Turns`
- [ ] Architect responses are color-coded differently from user input on stderr
- [ ] Draft updates from `ParseCanvasResponse` are applied via `ApplyUpdate` and a change summary is printed
- [ ] Failed architect invocations print the error and continue the loop (no crash)
- [ ] Malformed architect responses show the raw text and log the parse error
- [ ] Context cancellation triggers graceful shutdown
- [ ] `go test ./internal/canvas/...` passes with mock invoker tests
- [ ] `go vet ./internal/canvas/...` reports no issues

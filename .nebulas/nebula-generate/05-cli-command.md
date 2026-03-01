+++
id = "cli-command"
title = "Add `quasar nebula generate` Cobra subcommand"
type = "feature"
priority = 2
depends_on = ["multi-phase-architect", "nebula-writer"]
scope = ["cmd/nebula_generate.go", "cmd/nebula_generate_test.go", "cmd/nebula.go"]
+++

## Problem

The generation pipeline (`AnalyzeCodebase`, `Generate`, `WriteNebula`) and its supporting infrastructure exist in `internal/nebula/`, but there is no CLI entry point. Users need a `quasar nebula generate "Add JWT authentication"` command that wires up the invoker, runs the pipeline, and writes the result to `.nebulas/<name>/`.

The existing nebula subcommand infrastructure in `cmd/nebula.go` uses a `nebulaSubcmds` slice of `nebulaSubcmd` structs, each with `use`, `short`, `args`, `flags`, and `run` fields. The `init()` function iterates this slice and registers each as a Cobra subcommand of `nebulaCmd`. The generate command must follow this established pattern.

The command needs several flags: `--name` (nebula name override, defaulting to a slugified version of the prompt), `--output` (output directory override), `--model` (model override), `--force` (overwrite existing), `--budget` (generation budget cap), and `--dry-run` (show what would be generated without writing). It must also construct the `claude.Invoker` and pass it through to `Generate`.

## Solution

### 1. Register the Subcommand

Add a new entry to the `nebulaSubcmds` slice in `cmd/nebula.go`:

```go
{
    use:   "generate <prompt>",
    short: "Generate a complete nebula from a natural-language description",
    args:  cobra.ExactArgs(1),
    flags: addNebulaGenerateFlags,
    run:   runNebulaGenerate,
},
```

### 2. Command Implementation

Create `cmd/nebula_generate.go` with the command logic:

```go
// addNebulaGenerateFlags registers CLI flags for the generate subcommand.
func addNebulaGenerateFlags(cmd *cobra.Command) {
    cmd.Flags().String("name", "", "Nebula name (default: derived from prompt)")
    cmd.Flags().String("output", "", "Output directory (default: .nebulas/<name>)")
    cmd.Flags().String("model", "", "Model override for the architect agent")
    cmd.Flags().Float64("budget", 10.0, "Max budget in USD for nebula generation")
    cmd.Flags().Bool("force", false, "Overwrite existing nebula directory")
    cmd.Flags().Bool("dry-run", false, "Preview generated nebula without writing to disk")
}

// runNebulaGenerate implements the `quasar nebula generate` command.
func runNebulaGenerate(cmd *cobra.Command, args []string) error
```

Implementation flow for `runNebulaGenerate`:

1. **Parse flags**: Extract `--name`, `--output`, `--model`, `--budget`, `--force`, `--dry-run` from the command.
2. **Derive nebula name**: If `--name` is empty, derive it from the prompt using a `slugify` helper (lowercase, replace spaces with hyphens, strip non-alphanumeric characters, truncate to 50 chars).
3. **Determine output directory**: If `--output` is empty, default to `.nebulas/<name>`.
4. **Create invoker**: Construct a `claude.Invoker` using config values (same pattern as `cmd/run.go` or `cmd/nebula_apply.go`).
5. **Run codebase analysis**: Call `nebula.AnalyzeCodebase(ctx, workDir, snapshot.DefaultMaxSize)`. Print progress to stderr via `ui.Printer`.
6. **Run generation**: Call `nebula.Generate(ctx, invoker, nebula.GenerateRequest{...})`. Print progress updates to stderr.
7. **Handle dry-run**: If `--dry-run`, print the generated manifest and phase specs to stderr with formatting (phase ID, title, depends_on, scope) and exit without writing.
8. **Write to disk**: Call `nebula.WriteNebula(result, outputDir, nebula.WriteOptions{Overwrite: force})`.
9. **Print summary**: Print the generated nebula path, phase count, and total cost to stderr.
10. **Suggest next steps**: Print `Run 'quasar nebula validate <path>' to verify.` to stderr.

### 3. Slugify Helper

```go
// slugify converts a human-readable prompt into a kebab-case nebula name.
// It lowercases, replaces spaces/underscores with hyphens, strips
// non-alphanumeric characters, collapses consecutive hyphens, and truncates
// to maxLen characters.
func slugify(prompt string, maxLen int) string
```

### 4. Invoker Construction

Follow the existing pattern in other nebula commands for creating the invoker. Look at how `cmd/nebula_apply.go` or `cmd/run.go` constructs the `claude.Invoker`:

```go
invoker := &claude.Invoker{
    ClaudePath: viper.GetString("claude_path"),
    Verbose:    viper.GetBool("verbose"),
}
if err := invoker.Validate(); err != nil {
    return fmt.Errorf("claude CLI not available: %w", err)
}
```

### 5. Progress Output

Use `ui.Printer` for all human-readable output to stderr:

```go
p := ui.NewPrinter()
p.Info("Analyzing codebase...")
// ... after analysis
p.Info("Generating nebula %q from prompt...", name)
// ... after generation
p.Success("Generated %d phases in %s", len(result.Phases), outputDir)
p.Info("Total generation cost: $%.4f", result.CostUSD)
p.Info("Run 'quasar nebula validate %s' to verify.", outputDir)
```

### Testing

Create `cmd/nebula_generate_test.go`:

- **Slugify tests**: Table-driven tests for `slugify` with various inputs:
  - `"Add JWT authentication"` -> `"add-jwt-authentication"`
  - `"Fix bug #123 in parser!!!"` -> `"fix-bug-123-in-parser"`
  - `"   spaces   everywhere   "` -> `"spaces-everywhere"`
  - Very long string truncated to maxLen
- **Flag parsing**: Verify that all flags are registered and have correct defaults.
- **Dry-run**: Test that `--dry-run` does not create any files on disk (mock the invoker to return canned output).

## Files

- `cmd/nebula_generate.go` — New file: `addNebulaGenerateFlags`, `runNebulaGenerate`, `slugify`
- `cmd/nebula_generate_test.go` — New file: tests for slugify, flag registration, and dry-run behavior
- `cmd/nebula.go` — Modify: add generate entry to `nebulaSubcmds` slice

## Acceptance Criteria

- [ ] `quasar nebula generate "Add JWT authentication"` invokes the generation pipeline and writes a nebula to `.nebulas/add-jwt-authentication/`
- [ ] `--name custom-name` overrides the derived nebula name
- [ ] `--output /tmp/my-nebula` overrides the output directory
- [ ] `--model claude-sonnet-4-20250514` passes the model override to the architect agent
- [ ] `--budget 5.0` caps generation cost at $5
- [ ] `--force` allows overwriting an existing nebula directory
- [ ] `--dry-run` prints the generated phases to stderr without writing any files
- [ ] `slugify` correctly converts prompts to kebab-case names
- [ ] Command follows the existing `nebulaSubcmds` registration pattern in `cmd/nebula.go`
- [ ] Invoker construction follows established patterns from other commands
- [ ] All progress and error output goes to stderr via `ui.Printer`
- [ ] `go test ./cmd/...` passes
- [ ] `go vet ./...` reports no issues

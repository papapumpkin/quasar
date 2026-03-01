+++
id = "config"
title = "Add configuration knobs for cache optimization behavior"
type = "task"
priority = 2
depends_on = ["cross-invocation", "cache-telemetry"]
labels = ["quasar", "cache", "cost-optimization"]
scope = ["internal/config/config.go", "cmd/**"]
+++

## Problem

The cache optimization work from phases 01-03 is always-on by default. While the changes are backward-compatible (same behavior when `ProjectContext` is empty), operators need knobs to:

1. **Disable** cache optimization entirely (for debugging or when caching is counterproductive for very short tasks)
2. **Control** how much stable content goes into the system prompt prefix (larger prefix = more cached, but also more tokens on cache miss)
3. **Enable verbose cache logging** independently of the global `--verbose` flag
4. **Set the project context source** — currently `Scanner.Scan()` is the only source, but operators might want to provide a static file path instead

### Current config

`config.Config` (in `internal/config/config.go`) has these relevant fields:

```go
type Config struct {
    ClaudePath           string   `mapstructure:"claude_path"`
    BeadsPath            string   `mapstructure:"beads_path"`
    WorkDir              string   `mapstructure:"work_dir"`
    MaxReviewCycles      int      `mapstructure:"max_review_cycles"`
    MaxBudgetUSD         float64  `mapstructure:"max_budget_usd"`
    Model                string   `mapstructure:"model"`
    CoderSystemPrompt    string   `mapstructure:"coder_system_prompt"`
    ReviewerSystemPrompt string   `mapstructure:"reviewer_system_prompt"`
    Verbose              bool     `mapstructure:"verbose"`
    LintCommands         []string `mapstructure:"lint_commands"`
}
```

There is no cache-related configuration. The `MaxContextTokens` field on the `Loop` struct controls context budget but is not exposed in config.

## Solution

### 1. Add cache config fields to `Config`

```go
type Config struct {
    // ... existing fields ...

    // CacheOptimization enables prompt cache optimization. When true, system
    // prompts are pre-computed once per phase and project context is placed
    // exclusively in the system prompt prefix for Anthropic cache hits.
    // Default: true.
    CacheOptimization bool `mapstructure:"cache_optimization"`

    // CacheVerbose enables detailed cache hit/miss logging to stderr,
    // independent of the global Verbose flag. Useful for diagnosing
    // cache effectiveness without full verbose output.
    // Default: false.
    CacheVerbose bool `mapstructure:"cache_verbose"`

    // ProjectContextPath overrides the automatic Scanner.Scan() with a
    // static file whose contents are used as the project context prefix.
    // When set, the scanner is not invoked and this file is read instead.
    // The file must produce deterministic content for cache effectiveness.
    // Default: "" (use Scanner.Scan()).
    ProjectContextPath string `mapstructure:"project_context_path"`

    // MaxContextTokens sets the token budget for context injection.
    // Controls how much project context and fabric state is included.
    // Default: 10000 (from snapshot.DefaultMaxContextTokens).
    MaxContextTokens int `mapstructure:"max_context_tokens"`
}
```

### 2. Register defaults in `Load()`

```go
func Load() (Config, error) {
    // ... existing defaults ...
    viper.SetDefault("cache_optimization", true)
    viper.SetDefault("cache_verbose", false)
    viper.SetDefault("project_context_path", "")
    viper.SetDefault("max_context_tokens", 10000)
    // ...
}
```

### 3. Add CLI flags

In the `run` command (`cmd/run.go` or equivalent), add flags:

```go
cmd.Flags().Bool("cache-optimization", true, "Enable prompt cache optimization (stable system prompt prefix)")
cmd.Flags().Bool("cache-verbose", false, "Log cache hit/miss details to stderr")
cmd.Flags().String("project-context-path", "", "Path to static project context file (overrides scanner)")
cmd.Flags().Int("max-context-tokens", 10000, "Token budget for context injection")
```

Bind to viper:

```go
viper.BindPFlag("cache_optimization", cmd.Flags().Lookup("cache-optimization"))
viper.BindPFlag("cache_verbose", cmd.Flags().Lookup("cache-verbose"))
viper.BindPFlag("project_context_path", cmd.Flags().Lookup("project-context-path"))
viper.BindPFlag("max_context_tokens", cmd.Flags().Lookup("max-context-tokens"))
```

### 4. Environment variable support

Following the existing `QUASAR_*` convention:

- `QUASAR_CACHE_OPTIMIZATION=false` — disable cache optimization
- `QUASAR_CACHE_VERBOSE=true` — enable cache logging
- `QUASAR_PROJECT_CONTEXT_PATH=/path/to/context.md` — static context file
- `QUASAR_MAX_CONTEXT_TOKENS=20000` — increase token budget

These are automatically supported by Viper's `SetEnvPrefix("QUASAR")` + `AutomaticEnv()`.

### 5. Wire config into Loop construction

In the command that constructs the `Loop` (likely `cmd/run.go`), use the new config fields:

```go
// Load project context based on config
var projectContext string
if cfg.ProjectContextPath != "" {
    data, err := os.ReadFile(cfg.ProjectContextPath)
    if err != nil {
        return fmt.Errorf("failed to read project context file: %w", err)
    }
    projectContext = string(data)
} else {
    scanner := &snapshot.Scanner{WorkDir: cfg.WorkDir}
    ctx, err := scanner.Scan(context.Background())
    if err != nil {
        // Non-fatal: log and continue without project context
        ui.Error(fmt.Sprintf("project context scan failed: %v", err))
    }
    projectContext = ctx
}

loop := &loop.Loop{
    // ... existing fields ...
    ProjectContext:   projectContext,
    MaxContextTokens: cfg.MaxContextTokens,
    CacheOptimization: cfg.CacheOptimization,
    CacheVerbose:      cfg.CacheVerbose,
}
```

### 6. Guard cache optimization in Loop

When `CacheOptimization` is false, fall back to the pre-optimization behavior (rebuild system prompts per cycle, include project context in user prompt):

```go
func (l *Loop) runLoop(ctx context.Context, beadID, taskDescription string) (*TaskResult, error) {
    if l.CacheOptimization {
        // Pre-compute system prompts once for the entire phase (cache-optimized path)
        opts := agent.PromptOpts{
            FabricEnabled:  l.FabricEnabled,
            TaskID:         l.TaskID,
            ProjectContext: l.ProjectContext,
        }
        l.cachedCoderSystemPrompt = agent.BuildSystemPrompt(l.CoderPrompt, opts)
        l.cachedReviewerSystemPrompt = agent.BuildSystemPrompt(l.ReviewPrompt, opts)
    }
    // ...
}
```

### 7. `.quasar.yaml` example

```yaml
# Cache optimization (default: true)
cache_optimization: true
cache_verbose: false
# project_context_path: ./my-context.md  # uncomment to use static file
max_context_tokens: 10000
```

## Files

- `internal/config/config.go` — add `CacheOptimization`, `CacheVerbose`, `ProjectContextPath`, `MaxContextTokens` fields and defaults
- `cmd/run.go` (or equivalent command file) — add CLI flags, bind to viper, wire into Loop construction
- `internal/loop/loop.go` — add `CacheOptimization` and `CacheVerbose` fields to `Loop` struct, guard cache-optimized path
- `internal/config/config_test.go` — test default values for new fields, test override via viper

## Acceptance Criteria

- [ ] `Config` struct has `CacheOptimization` (default true), `CacheVerbose` (default false), `ProjectContextPath` (default ""), `MaxContextTokens` (default 10000)
- [ ] CLI flags `--cache-optimization`, `--cache-verbose`, `--project-context-path`, `--max-context-tokens` are registered and bound to viper
- [ ] Environment variables `QUASAR_CACHE_OPTIMIZATION`, `QUASAR_CACHE_VERBOSE`, `QUASAR_PROJECT_CONTEXT_PATH`, `QUASAR_MAX_CONTEXT_TOKENS` work
- [ ] When `CacheOptimization` is false, system prompts are rebuilt per cycle (legacy behavior)
- [ ] When `ProjectContextPath` is set, that file is read instead of running `Scanner.Scan()`
- [ ] `MaxContextTokens` is wired through to `Loop.MaxContextTokens`
- [ ] Config precedence follows: CLI flags > env vars > `.quasar.yaml` > defaults
- [ ] `go test ./internal/config/...` passes
- [ ] `go vet ./...` clean

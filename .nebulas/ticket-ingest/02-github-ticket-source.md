+++
id = "github-ticket-source"
title = "Implement the GitHub Issues TicketSource adapter (read-only, via gh CLI)"
type = "task"
priority = 1
depends_on = ["integration-layer"]
scope = [
    "internal/integrations/github/github.go",
    "internal/integrations/github/github_test.go",
    "internal/integrations/github/parse.go",
    "internal/integrations/github/parse_test.go",
    "internal/integrations/github/exec.go",
    "internal/integrations/github/exec_test.go",
]
+++

## Problem

With the integration layer in place from phase 1, the first concrete `TicketSource` adapter needs to ship: GitHub Issues. This is the only adapter Quasar needs at launch — Jira/Linear/etc. can follow the same shape later without coordination.

The adapter must be read-only (the user's directive: vanilla `git` for all output; `gh` is allowed only inside this package for ticket reading), Docker-friendly (token via env/file, or fallback to `gh auth token` for local dev), and resilient to the gh CLI being missing (a clear error pointing at the install docs rather than an exec panic).

## Solution

### Package layout

Create `internal/integrations/github/`. Files:
- `github.go` — the `Source` struct that satisfies `integrations.TicketSource`, plus the init() that registers it
- `parse.go` — pure functions for parsing source-id strings and gh JSON output (separable for unit tests without gh installed)
- `exec.go` — the thin shell-out wrapper (`runGH(ctx, args…) ([]byte, error)`) with mockable hook for tests

### Source-ID format

Three forms, parsed by `ParseSourceID`:

| input | repo | number |
|---|---|---|
| `42` | from config (`[integrations.github].repo`) | 42 |
| `owner/repo#42` | `owner/repo` | 42 |
| `#42` | error: ambiguous |

When the input is bare `<n>` and no `repo` is configured AND auto-detection from `git remote get-url origin` fails, return a `MissingRepoError` with a message naming both fixes (`[integrations.github].repo = "..."` or use the explicit `owner/repo#42` form).

### `Source` struct

```go
type Source struct {
    repo        string // "owner/repo" used when sourceID is bare number
    token       string // resolved at construction; empty -> fall back to gh's own auth
    runGH       func(ctx context.Context, args ...string) ([]byte, error) // injectable for tests
}

func New(cfg map[string]any, secrets integrations.SecretResolver) (*Source, error)

func (s *Source) Name() string { return "github" }
func (s *Source) Fetch(ctx context.Context, sourceID string) (*integrations.Ticket, error)
```

`New` is the constructor registered with the integration registry. It:
1. Reads `repo` from config; if empty, attempts auto-detection via `git remote get-url origin` (defer to `internal/gitops/` once phase 3 lands; for this phase, do the parse inline — `git@github.com:owner/repo.git` and `https://github.com/owner/repo.git` shapes)
2. Resolves the token via `SecretResolver.Resolve({Env: cfg["token_env"], File: cfg["token_file"]})`
3. Validates that the `gh` CLI exists on PATH (via `exec.LookPath`); returns a `GHNotInstalledError` with install URL if missing
4. Returns a `*Source` with default `runGH` wired to `exec.CommandContext`

### `Fetch` implementation

`Fetch(ctx, sourceID)`:
1. `repo, number := ParseSourceID(sourceID, s.repo)` — error path returns immediately
2. Issue body + metadata: `gh issue view <number> --repo <repo> --json number,title,body,state,labels,assignees,url`
3. Issue comments: `gh issue view <number> --repo <repo> --json comments`
4. Linked PRs/cross-refs (best-effort, non-fatal): `gh issue view <number> --repo <repo> --json timelineItems` and filter for `CrossReferencedEvent` / `ReferencedEvent` entries pointing at PRs
5. Assemble into a `*integrations.Ticket`:
   ```go
   t := &integrations.Ticket{
       SourceName:     "github",
       SourceID:       fmt.Sprintf("%s#%d", repo, number),
       Number:         number,
       Title:          parsed.Title,
       Body:           parsed.Body,
       State:          parsed.State,
       Labels:         parsed.LabelNames(),
       Assignee:       parsed.PrimaryAssignee(),
       URL:            parsed.URL,
       Comments:       parsed.Comments,
       LinkedWork:     parsed.LinkedPRURLs,
       SourceMetadata: nil, // populated if we later expose milestone/project
   }
   ```

If the token is non-empty, set `GH_TOKEN=<token>` in the `gh` invocation env. If empty, let `gh` use its own auth chain (gh config → keychain → device flow already done out-of-band).

### Authentication fallback chain

| token_file set | token_env set | env var present | behavior |
|---|---|---|---|
| yes | (any) | (any) | read file (file takes precedence per phase 1) |
| no | yes | yes | use env value |
| no | yes | no | fall back to `gh`'s own auth (run `gh auth status` once at construction, log warning if not authenticated, defer hard error to first Fetch) |
| no | no | (any) | fall back to `gh`'s own auth, same as above |

In all fall-back-to-gh cases, do NOT set `GH_TOKEN` in subprocess env; let gh resolve its own credentials.

### Error types

Define typed errors:
- `GHNotInstalledError` — gh binary missing from PATH
- `MissingRepoError` — bare number with no configured/detectable repo
- `TicketNotFoundError` — gh returned 404 or `gh issue view` exit code 1 with "could not resolve"
- `AuthFailedError` — gh exit code 4 (authentication required) or stderr containing "gh auth login"

These satisfy `errors.Is` for downstream UX handling. Surface them through `errors.As` rather than string matching.

### Registration

`github.go` ends with:
```go
func init() {
    integrations.Default().RegisterTicketSource("github", func(cfg map[string]any, secrets integrations.SecretResolver) (integrations.TicketSource, error) {
        return New(cfg, secrets)
    })
}
```

The package is imported by the cmd layer (phase 5) so the init fires; no other consumers should need to import it directly.

### Testing strategy

The package is structured around `parse.go` (pure functions, no I/O) and `exec.go` (thin shell-out, swap-able). Tests should be **>80% coverage on `parse.go`** and use a fake `runGH` for `Fetch` paths — no real `gh` invocations in `go test`.

`parse_test.go`: table-driven cases for `ParseSourceID` (all three input shapes including error cases), `LabelNames`, `PrimaryAssignee`, comment ordering, linked-PR extraction.

`github_test.go`: 
- `Fetch` happy path with a recorded gh JSON fixture under `testdata/gh-issue-42.json`
- `Fetch` with token from env vs token from file (mock `SecretResolver`)
- `Fetch` returns `TicketNotFoundError` when `runGH` returns exit 1 + "could not resolve" stderr
- `Fetch` returns `AuthFailedError` when `runGH` returns exit 4
- `New` with missing gh binary returns `GHNotInstalledError` (exec.LookPath stub)

`exec_test.go`: smoke test for the default `runGH` — actually invokes `gh --version` (skip with `testing.Short()`). This is the one place we tolerate a real subprocess.

## Files

- `internal/integrations/github/github.go` (new) — Source struct, New, Fetch, init() registration
- `internal/integrations/github/parse.go` (new) — ParseSourceID, JSON decoders, label/assignee helpers, linked-PR extraction
- `internal/integrations/github/exec.go` (new) — runGH default implementation; injectable hook
- `internal/integrations/github/github_test.go` (new) — adapter behavior with fake runGH
- `internal/integrations/github/parse_test.go` (new) — pure-function table tests
- `internal/integrations/github/exec_test.go` (new) — smoke test (skipped in -short mode)
- `internal/integrations/github/testdata/gh-issue-42.json` (new) — recorded gh JSON fixture for happy-path Fetch
- `internal/integrations/github/testdata/gh-issue-comments.json` (new) — recorded comments JSON

## Acceptance Criteria

- [ ] `internal/integrations/github/` package compiles
- [ ] `Source` satisfies `integrations.TicketSource` (interface compile-time assertion: `var _ integrations.TicketSource = (*Source)(nil)`)
- [ ] `init()` registers the adapter under the name "github" in the default registry
- [ ] `ParseSourceID("42", "owner/repo")` returns `("owner/repo", 42, nil)`
- [ ] `ParseSourceID("acme/widgets#7", "")` returns `("acme/widgets", 7, nil)`
- [ ] `ParseSourceID("42", "")` returns `MissingRepoError`
- [ ] `ParseSourceID("#42", "owner/repo")` returns an "ambiguous" error
- [ ] `New` returns `GHNotInstalledError` when `gh` is not on PATH (test via fake `exec.LookPath`)
- [ ] `Fetch` with the happy-path fixture returns a `Ticket` with all expected fields populated; `SourceID == "<repo>#<number>"`
- [ ] `Fetch` with a token-failing fake `runGH` returns `AuthFailedError`
- [ ] `Fetch` with a 404 fake returns `TicketNotFoundError`
- [ ] Token resolution honors phase 1's precedence (file > env > empty → gh fallback)
- [ ] No production code in this package calls `exec.Command("git", …)` — git operations belong in phase 3's `internal/gitops/`
- [ ] `go test ./internal/integrations/github/...` exits 0 with no real gh invocations (excluding the explicit smoke test in `-short` mode)
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all exit 0

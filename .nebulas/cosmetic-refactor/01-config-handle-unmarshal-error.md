+++
id = "config-handle-unmarshal-error"
title = "Handle silently discarded unmarshal error in config.Load"
type = "bug"
priority = 1
scope = ["internal/config/"]
+++

## Problem

In `internal/config/config.go`, `Load()` silently discards the error from `viper.Unmarshal`:

```go
var cfg Config
_ = viper.Unmarshal(&cfg)
return cfg
```

This means malformed YAML fields (wrong types, unknown keys with strict mode, etc.) produce a zero-value `Config` with no warning. A user could have a typo in `.quasar.yaml` and never know their config is being ignored.

## Solution

Return the error from `viper.Unmarshal` so callers can handle it. `Load()` should return `(Config, error)`. Update all call sites (in `cmd/`) to check and report the error.

1. Change `Load()` signature to `func Load() (Config, error)`.
2. Replace `_ = viper.Unmarshal(&cfg)` with proper error handling and return.
3. Update all callers in `cmd/` to handle the new error return (likely `root.go` or wherever `config.Load()` is called).

## Files

- `internal/config/config.go` — change `Load()` to return error
- `cmd/root.go` (or wherever `config.Load()` is invoked) — handle the error

## Acceptance Criteria

- [ ] `config.Load()` returns `(Config, error)`
- [ ] All callers handle the error (no `_ =` on the error)
- [ ] `go test ./...` passes
- [ ] `go vet ./...` passes

+++
id = "config-error"
title = "Make config.Load() return errors instead of swallowing them"
+++

Change `config.Load()` in `internal/config/config.go` to return `(Config, error)`
instead of just `Config`. The `viper.Unmarshal` failure should be propagated.

Update all callers in `cmd/` to handle the returned error.

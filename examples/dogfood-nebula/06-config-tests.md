+++
id = "config-tests"
title = "Add unit tests for config.Load()"
depends_on = ["config-error"]
+++

Add unit tests in `internal/config/config_test.go` for `config.Load()` that
verify:

1. Default values are applied correctly
2. `QUASAR_*` environment variable overrides take effect
3. The function returns an error for invalid configuration (after the error
   return refactor)

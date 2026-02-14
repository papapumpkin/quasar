+++
id = "makefile"
title = "Add a Makefile with standard targets"
+++

Add a `Makefile` at the project root with these targets:

- `build` — produce `./quasar` via `go build -o quasar .`
- `test` — run `go test ./...`
- `fmt` — run `gofmt -w .`
- `vet` — run `go vet ./...`
- `clean` — remove the `quasar` binary
- `all` (default) — fmt, vet, test, build

Keep it simple — no variables or conditional logic needed.

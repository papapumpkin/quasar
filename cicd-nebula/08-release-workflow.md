+++
id = "release-workflow"
title = "Create release workflow with cross-compiled binaries"
type = "task"
priority = 2
depends_on = ["ci-workflow"]
+++

## Problem

There is no automated release process. Creating releases with cross-compiled binaries is manual, error-prone, and inconsistent.

## Solution

Create a GitHub Actions workflow `.github/workflows/release.yml` triggered by `v*` tags. It cross-compiles binaries for linux/amd64, darwin/arm64, and darwin/amd64 using a matrix strategy, then creates a GitHub Release with the binaries attached via `softprops/action-gh-release@v2`.

### `.github/workflows/release.yml`

```yaml
name: Release

on:
  push:
    tags:
      - "v*"

permissions:
  contents: write

jobs:
  build:
    name: Build ${{ matrix.goos }}/${{ matrix.goarch }}
    runs-on: ubuntu-latest
    strategy:
      matrix:
        include:
          - goos: linux
            goarch: amd64
          - goos: darwin
            goarch: arm64
          - goos: darwin
            goarch: amd64
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Build
        env:
          GOOS: ${{ matrix.goos }}
          GOARCH: ${{ matrix.goarch }}
          VERSION: ${{ github.ref_name }}
        run: |
          bash scripts/build.sh
          mv quasar quasar-${{ matrix.goos }}-${{ matrix.goarch }}
      - uses: actions/upload-artifact@v4
        with:
          name: quasar-${{ matrix.goos }}-${{ matrix.goarch }}
          path: quasar-${{ matrix.goos }}-${{ matrix.goarch }}

  release:
    name: Create Release
    runs-on: ubuntu-latest
    needs: build
    steps:
      - uses: actions/download-artifact@v4
        with:
          merge-multiple: true
      - uses: softprops/action-gh-release@v2
        with:
          generate_release_notes: true
          files: quasar-*
```

## Files to Create

- `.github/workflows/release.yml` — Release workflow

## Acceptance Criteria

- [ ] `.github/workflows/release.yml` exists
- [ ] Triggers only on `v*` tags
- [ ] Matrix builds for linux/amd64, darwin/arm64, darwin/amd64
- [ ] Uses `scripts/build.sh` for building (reuses build script from task 06)
- [ ] Passes `VERSION` from git tag via `github.ref_name`
- [ ] Uploads binaries as artifacts
- [ ] Creates GitHub Release via `softprops/action-gh-release@v2`
- [ ] Attaches all cross-compiled binaries to the release
- [ ] Permissions set to `contents: write`
- [ ] Workflow YAML is valid

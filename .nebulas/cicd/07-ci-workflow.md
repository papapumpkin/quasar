+++
id = "ci-workflow"
title = "Create CI workflow composing all checks"
type = "task"
priority = 2
depends_on = ["test-script-action", "vet-script-action", "lint-script-action", "fmt-script-action", "security-script-action", "build-script-action"]
+++

## Problem

There is no CI pipeline. Tests, vet, lint, fmt, and security checks are not run automatically on push or PR. Bad code can be merged without automated verification.

## Solution

Create a GitHub Actions workflow `.github/workflows/ci.yml` that runs all checks on push to `main` and on PRs targeting `main`. The vet, lint, test, fmt, and security checks run in parallel. The build step runs after all checks pass as a final gate.

### `.github/workflows/ci.yml`

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

permissions:
  contents: read

jobs:
  vet:
    name: Vet
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/vet

  lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/lint

  test:
    name: Test
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/test

  fmt:
    name: Format
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/fmt

  security:
    name: Security
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/security

  build:
    name: Build
    runs-on: ubuntu-latest
    needs: [vet, lint, test, fmt, security]
    steps:
      - uses: ./.github/actions/build
```

## Files to Create

- `.github/workflows/ci.yml` — CI workflow

## Acceptance Criteria

- [ ] `.github/workflows/ci.yml` exists
- [ ] Triggers on push to `main` and PRs to `main`
- [ ] vet, lint, test, fmt, security jobs run in parallel
- [ ] build job runs after all other jobs pass (`needs: [vet, lint, test, fmt, security]`)
- [ ] Permissions are set to `contents: read`
- [ ] Each job uses the corresponding composite action from `.github/actions/`
- [ ] Workflow YAML is valid

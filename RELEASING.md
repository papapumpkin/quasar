# Releasing Quasar

Quasar uses a tag-driven release model powered by [GoReleaser](https://goreleaser.com/). Pushing a version tag triggers automated cross-compilation, archiving, and GitHub release creation.

## Creating a Release

1. Choose a version following [semver](https://semver.org/): `vMAJOR.MINOR.PATCH`
2. Tag and push:

```bash
git tag v1.2.3
git push origin v1.2.3
```

3. The [Release workflow](.github/workflows/release.yml) runs CI checks, then builds and publishes:
   - `quasar_VERSION_linux_amd64.tar.gz`
   - `quasar_VERSION_darwin_arm64.tar.gz`
   - `quasar_VERSION_darwin_amd64.tar.gz`
   - `checksums.txt` (SHA256)
   - Auto-generated changelog grouped by conventional commit prefix

## Verifying a Release

- Check the [Actions tab](https://github.com/papapumpkin/quasar/actions) for workflow status
- Verify all three platform archives and `checksums.txt` are attached to the release
- Download and test:

```bash
gh release download v1.2.3 --pattern 'quasar_*_darwin_arm64.tar.gz'
tar xzf quasar_*.tar.gz
./quasar version
```

## Promoting to Homebrew

Not every release needs Homebrew promotion. To promote a release:

1. Go to **Actions > Promote to Homebrew > Run workflow**
2. Enter the tag (e.g. `v1.2.3`)
3. The workflow downloads archives, computes checksums, and pushes the formula to `papapumpkin/homebrew-tap`

Users can then install with:

```bash
brew tap papapumpkin/tap
brew install quasar
```

## One-Time Setup

These steps are needed once per GitHub organization:

1. **Create the tap repository**: `papapumpkin/homebrew-tap` on GitHub (public, with a `Formula/` directory)
2. **Create a PAT** with `repo` scope that can push to the tap repository
3. **Store the PAT** as a repository secret named `HOMEBREW_TAP_TOKEN` in the quasar repo

## Local Snapshot Builds

Build all targets locally without publishing:

```bash
goreleaser build --snapshot --clean
```

Binaries appear in `dist/`. Single-target build for faster iteration:

```bash
goreleaser build --snapshot --clean --single-target
```

## Changelog Quality

The release changelog is auto-generated from commit messages between tags. Using [conventional commits](https://www.conventionalcommits.org/) improves grouping:

- `feat:` — Features
- `fix:` — Bug Fixes
- `perf:` — Performance
- `refactor:` — Refactoring

Commits prefixed with `chore`, `ci`, `test`, or `style` are excluded from the changelog.

# update-release-workflow

Replace `.github/workflows/release.yml` with `goreleaser/goreleaser-action@v6`. Gate releases on CI by adding a `ci` job that calls `ci.yml` via `workflow_call`. Add `workflow_call:` trigger to `ci.yml`.

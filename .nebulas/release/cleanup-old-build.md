# cleanup-old-build

Delete `scripts/build.sh`. Update `.github/actions/build/action.yml` to use `goreleaser build --snapshot --clean --single-target` for CI verification builds.

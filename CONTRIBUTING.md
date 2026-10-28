# Contributing

Run `make ci` before pushing. It runs the same `lint`, `test`, and `race`
commands CI runs. CI stays authoritative; `make ci` is the whole local
contract.

CI runs each pull request through path-selected lanes
(`.github/workflows/ci.yml`). A docs-only change runs no lane. Everything
else a lane needs is in the workflow: `go.mod` is the Go version source,
`COVERAGE_FLOOR` (currently 90, may only rise) gates the race lane, and the
`ci-complete` aggregator is the only check the branch ruleset requires.

Three rules have no exceptions:

- No `.go` file may exceed 500 lines. The lint lane enforces it with a
  plain `wc -l` step. Split the file instead of asking for an exclusion.
- Lint runs on the whole repo under `.golangci.yml` (govet, staticcheck,
  ineffassign, unused, errcheck; gofumpt formatting). No baselines and no
  new-code-only mode: fix the finding.
- `manifest.yaml` keeps the all-zero placeholder image digest permanently.
  It is `go:embed`'d into the binary, so a committed real digest would
  describe an older build than the image carrying it. `manifest_test.go`
  fails on a real digest. Stamp the deployed digest at seed time
  (`collector seed --image-ref`, or `scripts/pin-digest.sh` to resolve it).
  The README "Releasing" section has the full flow.

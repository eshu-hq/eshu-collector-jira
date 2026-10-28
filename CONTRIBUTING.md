# Contributing

Run `make ci` before pushing. It runs the same `lint`, `test`, and `race`
commands CI runs. CI stays authoritative; `make ci` is the whole local
contract.

Shell scripts pass `shellcheck` (see the `shell` lane). Keep them
`set -euo pipefail` clean like `scripts/pin-digest.sh`.

## CI lanes

Job names are frozen because branch protection requires
`go-ci / ci-complete`. Lane bodies live in the pinned reusable workflow
`eshu-hq/.github/.github/workflows/go-collector-ci.yml` (see the `go-ci`
job in `.github/workflows/ci.yml`); check runs nest as `go-ci / <lane>`.
Artifacts are named per module: `coverage-jira` (14-day retention) and
`gosec-jira` (SARIF + log, 14-day retention).

| Lane | Runs when |
| --- | --- |
| `lint` | Go sources, `manifest.yaml`, module or lint-config changes, plus unclassified files |
| `test` | Above, plus fixtures and config the tests read, plus unclassified files |
| `race` | Go sources, module, lint-config, or workflow changes, plus unclassified files, plus a coverage artifact and floor check |
| `boundary` | Go sources, workflow changes, or unclassified files (standalone import boundary) |
| `shell` | Shell script changes |
| `image` | Go sources (the Dockerfile builds the binary from them), `Dockerfile`, `.dockerignore`, `manifest.yaml`, or module changes (build only, no push) |
| `govulncheck` | Dependency changes, pushes to `main`, weekly schedule |
| `gosec` | Go sources, workflow changes, pushes to `main`, weekly schedule; advisory, SARIF to the Security tab off-PR |
| `actionlint` | Workflow changes, pushes to `main`, weekly schedule |
| `ci-complete` | Always; fails on any needed lane failure, passes on legitimate skips |

Docs-only PRs (`README.md`, `LICENSE`) run no lane and stay mergeable
through `ci-complete`. Anything unclassified (for example `Makefile`,
`CODEOWNERS`, `.gitignore`) runs `lint`, `test`, `race`, and `boundary`.
`scripts/record-grants.sh` runs `shell` plus `test` because the grant
test opens it; `.github/dependabot.yml` runs the workflow lanes.
The weekly schedule runs `govulncheck`, `gosec`, and `actionlint`, and a
scheduled scan failure files (or updates) the weekly findings issue.

## Coverage floor

The `race` lane fails below `COVERAGE_FLOOR` in `ci.yml`. The floor is 90,
already at the epic's 90% target, and may only rise. Never lower it,
exclude packages silently, or delete assertions to move the number.

## Fixture-only CI

PR lanes never touch a live registry: tests run against inline fixtures.
`manifest.yaml` is embedded into the binary, so editing it runs the full
Go lanes. `grant.example.json` is read by the grant tests, so editing it
runs `test` only. `scripts/record-grants.sh` is opened by the grant test,
so editing it runs `shell` plus `test`. Base images stay digest-pinned;
re-pin with the `scripts/pin-digest.sh` flow and keep the Go base in sync
with `go.mod` (the `image` lane checks the sync).

## Three rules with no exceptions

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

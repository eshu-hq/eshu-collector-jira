# eshu-collector-jira

Status: pre-release. Do not rely on it in production yet.

This collector reads work items (issues) from one Jira Cloud site and hands Eshu a checked, redacted summary of what it found.

Eshu is the core system. A collector is a small helper program: it reads one outside system, turns what it finds into facts (small, typed records), and gives those facts back to Eshu. This collector only reads Jira. It never writes to Jira.

## What it collects

One run covers one Jira site, named by the job Eshu sends it (a claim). Through bounded Jira Cloud REST calls it reads issues changed in a time window (found with a JQL search; JQL is Jira's search language), each issue's change history and remote links (links to pages or code changes outside Jira), and project, issue-type, status, workflow, and field details for the site.

It returns 9 fact kinds, all at schema `1.0.0` with confidence `reported` (the source said so; Eshu did not check it directly):

| Fact kind | One fact describes |
| --- | --- |
| `work_item.record` | one issue |
| `work_item.transition` | one status change in an issue's history |
| `work_item.external_link` | one remote link on an issue |
| `work_item.project_metadata` | one project on the site |
| `work_item.issue_type_metadata` | one issue type on the site |
| `work_item.status_metadata` | one status on the site |
| `work_item.workflow_metadata` | one workflow on the site |
| `work_item.field_metadata` | one field on the site |
| `work_item.metadata_warning` | site details the collector could not read |

Sensitive text is hidden before anything leaves the collector: issue summaries, people names, account IDs, web addresses, and link titles become yes/no flags and fingerprints (short hashes that show two values match without showing the value).

A run ends as `complete`, `partial` (some site details were hidden or unsupported and became warnings), `retryable` (try again later, for example after a rate limit), or `terminal` (stop; a person must fix something first). A failed run returns no facts. Eshu spots duplicate work with a freshness hint: a digest of the time window and every fact returned. The collector never returns `unchanged`, because the host sends no earlier digest to compare with.

## How data flows

```mermaid
flowchart LR
  A["Jira Cloud site"] --> B["This collector"] --> C["Eshu host process"] --> D["Eshu core"]
```

Text version: Jira Cloud site → this collector → Eshu host process (checks and stores the result) → Eshu core. The collector never talks to Eshu's database directly.

## When to use it, and when not to

Use it when Eshu needs current Jira issue, history, link, and site details for one Jira Cloud site, read on a schedule Eshu controls. Do not use it to change anything in Jira, to read more than one site in one setup (use one setup per site), or as a general Jira API tool. It reads only what the fact list above covers.

## How Eshu runs it

Eshu's component host starts this program for one claim at a time: it sends one bounded `collector-sdk/v1alpha1` request on standard input (stdin) and reads one result from standard output (stdout).

```bash
collector --sdk-stdio   # serve one claim from stdin
```

The collector gets no database, graph, queue, or lease handle. The host builds the final fact envelope (the wrapper Eshu stores), checks the result, and owns claims and commits.

The published image is `ghcr.io/eshu-hq/eshu-collector-jira`. It holds one static binary with no shell, and the binary can install itself into a component folder (how a startup, or init, container sets it up):

```bash
collector seed --dest /component [--image-ref ghcr.io/eshu-hq/eshu-collector-jira@sha256:<digest>]
```

This writes `/component/collector` (mode 0755) and `/component/manifest.yaml`. `--image-ref` (or the `COLLECTOR_IMAGE_REF` environment variable; the flag wins) stamps the exact deployed image reference into the manifest, because an image cannot contain its own digest. The manifest in this repository keeps an all-zero placeholder digest on purpose. A bad image reference fails before any file is written, and the error never echoes the value. `scripts/pin-digest.sh <repo:tag>` only prints the `repo@sha256:...` reference of a pushed image; it edits nothing.

## Configuration

Copy `config.example.yaml`. It has three parts: `host` names the job identity Eshu plans, `process` says how Eshu starts the binary, and `config` holds this collector's own settings. Unknown keys are rejected, and the error does not name the bad key (the key could be a pasted secret), so compare your file with the example when a config is rejected.

| `config` key | Meaning |
| --- | --- |
| `site_id`, `base_url` | Jira site host and web address, for example `example.atlassian.net` and `https://example.atlassian.net` |
| `email_env`, `token_env` | Names of environment variables that hold the login email and API token |
| `jql` or `jql_env` | The JQL search text, or the name of a variable that holds it. Exactly one is required |
| `issue_limit`, `updated_lookback` | Most issues per claim; how far back to look for changes (for example `24h`) |
| `changelog_limit`, `remote_link_limit` | Most history items and remote links to read per issue |
| `metadata_limit` | Most site-detail records to read |
| `allow_empty_successor` | Leave `false`; `true` is refused for now (see below) |
| `limits.*` | Size and time caps for one claim (see below) |

`allow_empty_successor: true` would mean "an empty or much smaller result is correct; let it replace the old one." This build cannot send that promise yet, so it refuses the setting with failure class `unsupported_option` instead of pretending.

## Credentials

Secrets never go in the config file. `email_env`, `token_env`, and `jql_env` name environment variables the worker process already has — for example, `token_env: JIRA_API_TOKEN` means "read the token from the variable called `JIRA_API_TOKEN`." Each value must be a plain variable name (letters, digits, underscores; at most 128 bytes). Secret values never appear in facts, source addresses, logs, or status messages, and error messages name the config key, never the value.

## Time and size limits

Eshu gives each claim a deadline (the first lease expiry; heartbeat messages do not extend it) and stops the process there. The default lease is 60 seconds, set with `ESHU_COMPONENT_COLLECTOR_CLAIM_LEASE_TTL` on the worker; a large site needs more (the reference setup uses 5 minutes with a 30-second heartbeat). The collector stops starting new Jira calls shortly before the deadline (the smaller of 2 seconds or 5% of the time left), cancels the call in progress, and returns `retryable` with failure class `claim_deadline_exceeded` and no facts — dropping finished work stops a half-finished read from being stored as complete. With no deadline, `limits.claimTimeoutSeconds` (default 300) is the only time bound, and an already-passed deadline fails at once, before any Jira call.

Size caps live under `config.limits`: `maxRecordsPerClaim`, `maxPayloadBytes` (one fact), and `maxResultBytes` (the whole result). A large result can pass the host's default 4 MiB output cap, so raise `process.stdout_limit_bytes` and `limits.maxResultBytes` together. The collector fails with `result_budget_exceeded` rather than let the host drop an oversized result.

## What operators see

The main signal is the failure class on the work item, such as `rate_limited`, `invalid_config`, `unsupported_option`, or `claim_deadline_exceeded`. On current Eshu `main`, the worker also records each result as `eshu_dp_component_extension_*` metrics and one `component extension result` log line (including source wait time and fact counts), and it honors `retry_after_seconds`, the wait the collector asks for before a retry, capped at one hour.

The collector also writes one JSON line per claim to standard error (stderr) with the failed step, HTTP status, and counts. The host keeps only the byte count and drops the text, so that line helps only in a local run (`collector --sdk-stdio < request.json`) or in tests. To find why a config was rejected, run the binary locally with that config and a recorded request; the host records only the failure class. This collector emits no metrics or spans of its own: the manifest declares the prefix `eshu_dp_jira_collector_`, but nothing emits under it yet.

## Keeping the same identity when you switch

If this collector replaces Eshu's built-in Jira collector for a site, three setup values must match the old ones, or Eshu will treat the same site as new data:

| Setup value | Rule |
| --- | --- |
| `host.scope.id` | Must equal the old target's scope ID, or every stored key changes |
| `host.scope.kind` | Must be `jira_site` |
| `host.sourceSystem` | Must be `jira`, or the source label changes on every fact |

The activation instance ID (`--instance`) must be new and different from the built-in collector's instance ID; two collectors with one ID stop the coordinator from starting. Only the `collector_instance_id` field inside `work_item.record` changes; stored keys do not. Read the real values from the live built-in setup, never copy the placeholder (`jira:site:example`), and run only one of the two collectors per site.

## Permission to publish facts

All 9 fact kinds belong to Eshu core, so the host refuses them without a producer grant (written permission for this component to publish that kind). `grant.example.json` lists the 9 requests, each tied to this component and version and scoped to `jira`; record them with `scripts/record-grants.sh` (it calls `eshu component grant`). Eshu core issues grants; this repository only ships the request. For checks, `conformance_test.go` runs the public SDK harness over saved results in `testdata/conformance`; refresh them with `go test -update-conformance-fixtures -run TestConformanceFixturesAreCurrent .` after a deliberate output change.

## Eshu dependencies

Exact versions from `go.mod`. Do not substitute another SDK line.

| Module | Version |
| --- | --- |
| Go | `1.26.10` |
| `github.com/eshu-hq/eshu/sdk/go/collector` | `v0.2.0` |
| `github.com/eshu-hq/eshu/sdk/go/factschema` | `v0.2.0` |
| `gopkg.in/yaml.v3` | `v3.0.1` |

The component ID is `dev.eshu.collector.jira`, version `0.1.0`. It declares support for Eshu core `>=0.0.5 <0.2.0` and speaks `collector-sdk/v1alpha1` over the process adapter (see `manifest.yaml`). Check that range against your core version before enabling it; a mismatch makes the host refuse the component.

## What it never does

- It never writes to Jira or changes an issue, and it never stores anything itself — the host validates and commits.
- It never imports Eshu core internals or another collector; `boundary_test.go` fails the build if that changes.
- It never puts secrets, raw summaries, names, account IDs, or full web addresses into facts, logs, or status messages.

Three helper folders (`internal/stableid`, `internal/httpx`, `internal/repoid`) copy small Eshu core helpers that a standalone repo cannot import yet. Tests pin them to the original behavior; they will be replaced when the shared SDK helpers are published.

## Build, test, and release

```bash
go build ./... && go vet ./...
go test -race -count=1 ./...
make ci   # lint + test + race; the full local check CI runs
```

`scripts/run-conformance.sh` runs the same gate as CI (`.github/workflows/ci.yml`, reported as `go-ci / ci-complete`). Releases run from `.github/workflows/release.yml` on every push to `main` and on version tags (`vMAJOR.MINOR.PATCH`, optional pre-release suffix). Nothing runs for a pull request, and the workflow deploys nothing. A `main` push publishes the moving `main`, `latest`, and `sha-<full-sha>` image tags — use moving tags only where floating is intended and pin real deployments to digests. A version tag must point at the protected `main` head and match `metadata.version` in `manifest.yaml`, or nothing is published. The workflow builds `linux/amd64`, pushes the image, writes the digest to the job summary, signs it with cosign (keyless), and attaches build-provenance and SBOM attestations, using no secret beyond `GITHUB_TOKEN`.

After a release, copy the `ghcr.io/eshu-hq/eshu-collector-jira@sha256:<digest>` reference from the job summary into the deployment and check it before use:

```bash
cosign verify ghcr.io/eshu-hq/eshu-collector-jira@sha256:<digest> --certificate-identity-regexp '^https://github.com/eshu-hq/eshu-collector-jira/\.github/workflows/release\.yml@refs/tags/v' --certificate-oidc-issuer https://token.actions.githubusercontent.com
gh attestation verify oci://ghcr.io/eshu-hq/eshu-collector-jira@sha256:<digest> -R eshu-hq/eshu-collector-jira
```

After the first release, set the GHCR package to public: GitHub creates packages as private, and a cluster cannot pull a private package without credentials. Restrict who can create `v*` tags to maintainers as well.

## Docs and related projects

- `config.example.yaml` — full setup example with comments; `manifest.yaml` — component identity, fact list, and core range; `grant.example.json` — the 9 grant requests.
- [Eshu core](https://github.com/eshu-hq/eshu) — the host that runs this collector.
- [eshu-sdk](https://github.com/eshu-hq/eshu-sdk) — shared SDK modules. [eshu-deploy](https://github.com/eshu-hq/eshu-deploy) — deployment examples and reference setups.

## Contributing

See `CONTRIBUTING.md`. In short: run `make ci` before pushing; it runs the same lint, test, and race checks as CI.

## License

MIT. See `LICENSE`.

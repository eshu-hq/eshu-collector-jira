# eshu-collector-jira

Eshu Jira work-item collector, extracted from the Eshu core repository.

**Pre-release. Holds no producer authority until its reviewed cutover
(eshu-hq/eshu#8858).**

The collector runs through the Eshu component-extension host with the
`process` runner. The host writes one bounded `collector-sdk/v1alpha1` request
to stdin and reads one result from stdout. The collector gets no Postgres,
graph, queue, lease, or workflow handle, and it never imports
`github.com/eshu-hq/eshu/go/internal/...` (`boundary_test.go` enforces this).
The host builds the core fact envelope, validates the result, and owns claims
and commits.

## What it collects

For the Jira Cloud site named by the claim it reads, through bounded REST calls:

- changed issues for a windowed JQL search, with changelog items and remote
  links per issue;
- project, issue-type, status, workflow, and field metadata.

It emits the nine core `work_item.*` kinds at schema `1.0.0`:
`record`, `transition`, `external_link`, `project_metadata`,
`issue_type_metadata`, `status_metadata`, `workflow_metadata`,
`field_metadata`, and `metadata_warning`. Summaries, names, account IDs, URLs,
and titles are replaced by presence flags and fingerprints before emission.

Result states: `complete`; `partial` when metadata the site hid or does not
support became warning facts; `retryable` for rate limits, transport errors,
and 5xx; `terminal` for permission, missing, archived, bad-request, invalid
configuration, and exhausted-budget failures. A failed claim emits no facts.
The collector never returns `unchanged`: the host sends no previous digest, so
duplicate detection rides on `generation.freshness_hint`, which is a digest of
the collection window and every emitted fact.

## Configuration

`config.example.yaml` is an activation config. The `config` map holds one Jira
target: `site_id`, `base_url`, `email_env`, `token_env`, `jql` or `jql_env`,
`issue_limit`, `updated_lookback`, `changelog_limit`, `remote_link_limit`,
`metadata_limit`, and an optional `limits` block. Unknown keys fail closed.
`token_env` and `email_env` name environment variables the worker exports.
Secret values never appear in facts, source URIs, logs, or statuses.

`allow_empty_successor` is rejected: the published SDK result type cannot yet
carry the trusted-deletion attestation the in-tree collector uses.

A site with a large issue window returns more than the host's default 4 MiB
stdout cap. Raise `process.stdout_limit_bytes` and `limits.maxResultBytes`
together; the collector fails terminal with `result_budget_exceeded` rather
than letting the host drop an oversized result.

## Image and seeding

The image holds one static binary that installs itself, so an init container
needs no shell:

```bash
collector seed --dest /component [--image ghcr.io/eshu-hq/eshu-collector-jira@sha256:<digest>]
```

writes `/component/collector` (mode 0755) and `/component/manifest.yaml`.
`--image` stamps the deployed digest-pinned reference into the manifest's
artifact entry, because an image cannot embed its own digest.

## Producer grants

All nine kinds are core-owned, so the host refuses them without a live producer
grant. `grant.example.json` lists the nine requests. Each grant is bound to this
component id and version and scoped to the manifest's collector kind (`jira`),
not to a site: core matches a grant's scope against the collector kinds the
manifest declares. `scripts/record-grants.sh` records them with
`eshu component grant` in a component home. Core issues grants; this
repository only ships the request.

## Conformance

`conformance_test.go` runs the public SDK harness over the checked-in results in
`testdata/conformance` (complete, partial, retryable, terminal) with the
published payload schemas the manifest references. `go test
-update-conformance-fixtures -run TestConformanceFixturesAreCurrent .`
rewrites them after a deliberate output change.

`eshu component inspect`, `verify`, and `conform` load the manifest without
producer grants, so they reject any manifest that declares core-owned kinds
(`fact kind "work_item.record" is core-owned by Eshu ...`). `install` and
`enable` are grant-aware and pass once the nine grants are recorded. Until core
makes the other three grant-aware, conformance for this manifest is the Go
harness above.

## Build and test

```bash
go build ./...
go vet ./...
go test -race -count=1 ./...
```

`scripts/run-conformance.sh` runs the gate CI runs
(`.github/workflows/ci.yml`; the `ci-complete` job aggregates the others).

## Copied code

Three small packages are copies of Eshu core code that is not importable from
a standalone repository. Each is pinned by golden tests produced from the
in-tree functions, and each is to be replaced by the published eshu-sdk helper
when eshu-hq/eshu#8851 lands:

- `internal/stableid`: `facts.StableID`, so stable keys match the in-tree
  collector byte for byte.
- `internal/httpx`: the bounded HTTP, retry-after, and failure-classification
  helpers. It adds a response-size cap the in-tree helper lacks.
- `internal/repoid`: `repositoryidentity.CanonicalRepositoryID`, for
  `linked_repository_id` on pull-request and merge-request links.

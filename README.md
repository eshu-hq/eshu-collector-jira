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
`metadata_limit`, and an optional `limits` block. Unknown keys fail closed:
the rejection says `unknown field` and does not name the key (it can be a
pasted credential), so compare the config with `config.example.yaml`.
`token_env` and `email_env` name environment variables the worker exports.
Secret values never appear in facts, source URIs, logs, or statuses. The three
`*_env` keys must hold environment variable names (`^[A-Za-z_][A-Za-z0-9_]*$`,
at most 128 bytes); a rejection message names the config key and never quotes
what was configured, so a credential pasted into one of them cannot be echoed.

`allow_empty_successor` (default false, unchanged) is parsed. When true the claim
fails closed with a terminal result whose status class is `unsupported_option`.
The in-tree collector uses it to attest that an empty or sharply shrunken
successor generation is legitimate; sending that attestation needs
`Generation.TrustedDeletion`, which landed in eshu-hq/eshu after
`sdk/go/collector` v0.2.0, and this repository does not hand-marshal
unpublished wire fields. Removal condition: pin a collector SDK release that
carries `TrustedDeletion`, then replace the rejection in `config.go` with the
field.

A site with a large issue window returns more than the host's default 4 MiB
stdout cap. Raise `process.stdout_limit_bytes` and `limits.maxResultBytes`
together; the collector fails terminal with `result_budget_exceeded` rather
than letting the host drop an oversized result.

## Claim time budget

The host sets a claim's deadline to the **initial lease expiry** and kills the
process there. Heartbeats do not extend it. The worker's default lease TTL is
60 seconds (`ESHU_COMPONENT_COLLECTOR_CLAIM_LEASE_TTL`), so one claim has at
most that long to read the site unless the worker is configured for more. For a
site with a large issue window, raise the worker lease TTL (for example to
`5m`) and keep the heartbeat well inside it.

The collector stops starting provider calls at the deadline minus
`min(2s, 5% of the time left)` (a 60 s lease gives 58 s, a 10 s lease 9.5 s, a
1 s lease 950 ms), cancels the call in flight, and returns a `retryable` result
with failure class `claim_deadline_exceeded`. The result sets
`retry_after_seconds`, but the worker does not honor it yet (see "Operator
signals"), so the collector's retry guidance has no effect. The
result carries no facts: a deadline stop is never `partial`, and facts read so
far are dropped so an incomplete read cannot commit as a complete generation.
The next attempt starts over, so a site that cannot finish inside the lease
keeps failing with the same class until the lease TTL is raised. A missing
deadline leaves only `limits.claimTimeoutSeconds` (default 300) as the bound; a
deadline already passed fails at once without calling Jira.

## Operator signals

Today the extension host passes one thing from a claim's result to the work
item: the **failure class** (`rate_limited`, `invalid_config`,
`unsupported_option`, `claim_deadline_exceeded`, ...). The worker does not set
a status recorder, so everything else in the result's status row is dropped:

- **Dropped: source latency and fact count.** The collector fills them in, but
  no dashboard or work item shows them.
- **Dropped: `retry_after_seconds`.** The collector sends it on every retryable
  result, but the worker does not honor it yet, so a retryable result gets no
  retry delay from the collector's guidance.
- **Dropped: the stderr log line.** The collector writes one JSON line per
  claim to stderr: the config rejection reason, the failed operation
  (`search_issues`, `read_changelog`, ...), the HTTP status, and collection
  counters. The host's process runner keeps stderr only as a byte count and
  drops the text. The line is for a local run
  (`collector --sdk-stdio < request.json`) and for tests.
- **No spans or metrics.** The in-tree collector emitted a fetch span and fetch
  counters. This collector has no equivalent. The manifest declares
  `telemetry.metricsPrefix: eshu_dp_jira_collector_`, but nothing in this
  repository emits a metric under it.

eshu-hq/eshu#8908 tracks recording status rows and honoring the retry delay in
the worker. Until it lands, a rejected config shows up only as the failure
class on the work item; find the cause by running the collector with the
activation's config and a recorded request.

## Image and seeding

The image holds one static binary that installs itself, so an init container
needs no shell:

```bash
collector seed --dest /component [--image-ref ghcr.io/eshu-hq/eshu-collector-jira@sha256:<digest>]
```

writes `/component/collector` (mode 0755) and `/component/manifest.yaml`.
`--image-ref REF` (or the `COLLECTOR_IMAGE_REF` environment variable; the flag
wins) stamps the deployed digest-pinned reference into the manifest's artifact
entry, because an image cannot embed its own digest. The repository manifest keeps the all-zero
placeholder digest permanently (`manifest_test.go` fails on a real one), because
it is embedded in the binary and a committed digest could only describe an older
build. `scripts/pin-digest.sh <repo:tag>` only prints the `repo@sha256:...`
reference of a pushed image; it edits nothing. `REF` must match
`^[a-z0-9]+([._-][a-z0-9]+)*(:[0-9]+)?(/[a-z0-9]+([._-][a-z0-9]+)*)*@sha256:[0-9a-f]{64}$`
after one trailing newline is trimmed; anything else (quotes, `$`, `#`,
brackets, whitespace, a second newline) exits non-zero before a file is
written, and the error never echoes the value. The reference is written
literally and is the only manifest byte that changes. Without it the
repository placeholder digest is written unchanged.

## Cutover identity

Replacing the in-tree Jira collector with this one must not re-key a single
fact. These activation values have to equal what the in-tree collector uses for
the same site:

| Activation value | Must equal the in-tree value | If it differs |
| --- | --- | --- |
| `host.scope.id` | the in-tree target's `scope_id` (the `targets[].scope_id` in the in-tree collector instance configuration) | every stable key and `scope_id` changes: the site is ingested as a new scope |
| `host.scope.kind` | `jira_site` | the scope no longer matches the in-tree scope kind |
| `host.sourceSystem` | the in-tree source system, `jira` | `source_ref` and `source_system` change on every fact |
| activation instance id (`--instance`) | the in-tree collector instance id | `collector_instance_id` in the `work_item.record` payload changes |

The collector copies `scope_id`, `collector_instance_id`, and `source_system`
from the claim the host sends; it has no setting of its own for them. A
`scope_id` in `config` is only a cross-check and is rejected when it differs
from `host.scope.id`.

Before enabling an activation, read the three values from the live in-tree
collector instance configuration and write them into the activation. Do not
copy the placeholders in `config.example.yaml` (`jira:site:example`,
`jira-primary`). Run only one of the two collectors per site: with the same
scope and instance id they would write the same stable keys.

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

`eshu component inspect`, `verify`, and `conform` read the producer grants in
the component home (`--component-home`) since eshu-hq/eshu#8952, so they judge
this manifest once the nine grants are recorded with `scripts/record-grants.sh`.
Without the grants they reject it (`fact kind "work_item.record" is core-owned
by Eshu ...`), as `install` and `enable` do. The Go harness above is the proof
that runs in this repository.

The manifest omits `consumerContracts.reducer.phases` on purpose. The only
phase the SDK harness accepts for an optional component is
`source_evidence_only:no_graph_truth`, which would be false here: the nine
`work_item.*` kinds are core-owned and feed real reducers. `manifest_test.go`
fails if a phase list is added back.

## Releasing

`.github/workflows/release.yml` runs only when a tag `vMAJOR.MINOR.PATCH[-pre]`
is pushed. Nothing runs on a pull request or a branch push, and the workflow
deploys nothing.

Before the first tag, confirm the repository restricts who can create `v*`
tags (a tag ruleset limited to maintainers). The workflow runs on **any** commit
a `v*` tag points at, including one that is not on `main`, so tag creation is
the control that decides what gets published as a signed image.

1. Set `metadata.version` in `manifest.yaml` to the version you are releasing
   and merge that change to `main`. Then tag the merge commit and push the tag.
   The workflow refuses to publish unless the tag equals `v` plus that version
   (a `-pre-release` suffix on the tag is allowed when the manifest version has
   none), and it checks this before it builds or pushes anything.
2. The workflow builds `linux/amd64` (the only platform the manifest declares),
   pushes `ghcr.io/eshu-hq/eshu-collector-jira:<tag>`, writes the digest to the
   job summary, signs the digest with cosign (keyless), and attaches a SLSA
   build-provenance attestation and an SPDX SBOM attestation. It needs no
   secret beyond the job's `GITHUB_TOKEN`.
3. Copy the `ghcr.io/eshu-hq/eshu-collector-jira@sha256:<digest>` reference
   from the job summary into the activation's `seed.image`. The deployment
   stamps it into the manifest with `collector seed --image-ref`; the
   repository manifest is not edited.
4. Check the image before enabling an activation:

```bash
cosign verify ghcr.io/eshu-hq/eshu-collector-jira@sha256:<digest> \
  --certificate-identity-regexp '^https://github.com/eshu-hq/eshu-collector-jira/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
gh attestation verify oci://ghcr.io/eshu-hq/eshu-collector-jira@sha256:<digest> \
  -R eshu-hq/eshu-collector-jira
```

The GHCR package must be **public**: GitHub creates a package private by
default, so after the first release set its visibility to public (package
settings) before a cluster tries to pull it without credentials.

`compatibleCore` in `manifest.yaml` is enforced by release builds of Eshu core.
Check it against the core version of the target environment before enabling an
activation; a range that excludes that version makes the host refuse the
component.

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

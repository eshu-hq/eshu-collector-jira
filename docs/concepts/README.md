# Concepts: how the Jira collector works

One run covers one Jira site, named by the claim Eshu sends. Through
bounded Jira Cloud REST calls it reads issues changed in a time window
(found with a JQL search; JQL is Jira's search language), each issue's
change history and remote links (links to pages or code changes outside
Jira), and project, issue-type, status, workflow, and field details for
the site.

Sensitive text is hidden before anything leaves the collector. Issue
summaries, people's names, account IDs, web addresses, and link titles
become yes/no flags and fingerprints, meaning short hashes that show two
values match without showing the value itself.

## The facts it can hand back

The full table lives in the [facts reference](../reference/facts.md) and mirrors `manifest.yaml`. The manifest is the source of truth,
so if you change one you need to update the other in the same change.
All nine kinds sit at schema `1.0.0` with confidence `reported`, which
just means the source said so and Eshu hasn't checked it directly.


The component behind that table is `dev.eshu.collector.jira`, version
`0.1.0`, collector kind `jira`. It declares support for Eshu core
`>=0.0.5 <0.2.0` and speaks `collector-sdk/v1alpha1` over the process
adapter. It's worth checking that range against your core version
before you enable it, because a mismatch makes the host refuse the
component.

## Where it sits

It sits between one Jira Cloud site and the Eshu host process. The
collector never talks to Eshu's database directly. The picture is the
[system context diagram](../diagrams/README.md#system-context).

## What happens to one claim

The step-by-step flow is the
[claim lifecycle diagram](../diagrams/README.md#claim-lifecycle).
A run ends as `complete`, `partial` (some site details were hidden or
unsupported and became warnings), `retryable` (try again later, say
after a rate limit), or `terminal` (stop; a person needs to fix
something first). A failed run returns no facts. Eshu spots duplicate
work with a freshness hint, a digest of the time window and every fact
returned. The collector never returns `unchanged`, because the host
sends no earlier digest to compare with.

## Identity, and why grants matter

If this collector replaces Eshu's built-in Jira collector for a site,
three setup values have to match the old ones or Eshu will treat the
same site as brand new data. The full table lives in
[configuration](../reference/configuration.md), but the short version is: keep
`host.scope.id`, keep `host.scope.kind` as `jira_site`, and keep
`host.sourceSystem` as `jira`. The activation instance ID works the
other way round. It must be new, because two collectors sharing one ID
stop the coordinator from starting. Only the `collector_instance_id`
field inside `work_item.record` changes; stored keys don't.

All nine fact kinds belong to Eshu core, so the host won't accept them
without a producer grant for each one. Think of a grant as written
permission for this component to publish that kind.
`grant.example.json` lists the nine requests, each tied to this
component and version and scoped to `jira`, and
`scripts/record-grants.sh` records them (it calls
`eshu component grant`). Core is the one that issues grants; this repo
only ships the request. If you're checking output,
`conformance_test.go` runs the public SDK harness over saved results in
`testdata/conformance`, and you can refresh those with
`go test -update-conformance-fixtures -run TestConformanceFixturesAreCurrent .`
after a deliberate output change.

## Lines it won't cross

It never writes to Jira or changes an issue, and it never stores
anything itself. The host validates and commits. It also never imports
Eshu core internals or another collector, and `boundary_test.go` fails
the build if that ever changes. Secrets, raw summaries, names, account
IDs, and full web addresses stay out of facts, logs, and status
messages.

One honest footnote: three helper folders (`internal/stableid`,
`internal/httpx`, `internal/repoid`) copy small Eshu core helpers that
a standalone repo can't import yet. Tests pin them to the original
behaviour, and they'll go away when shared SDK helpers are published.

## Pictures

The diagrams for this page (system context and claim lifecycle) live
in one place: [diagrams](../diagrams/README.md#system-context) and
[the claim lifecycle](../diagrams/README.md#claim-lifecycle). Each
one has a short text version next to it.

# Configuration reference: Jira collector

Start from the copyable [example config](../../config.example.yaml); component identity lives in the [manifest](../../manifest.yaml). For the facts these settings produce, see [facts](facts.md).

Start from `config.example.yaml` and copy it. The file has three parts:
`host` names the job identity Eshu plans, `process` says how Eshu
starts the binary, and `config` holds this collector's own settings.
Unknown keys are rejected, and the error won't name the bad key (the
key could be a pasted secret), so if a config gets refused, compare
your file with the example side by side.

## The settings

| `config` key | What it does |
| --- | --- |
| `site_id`, `base_url` | Jira site host and web address, for example `example.atlassian.net` and `https://example.atlassian.net` |
| `email_env`, `token_env` | Names of environment variables that hold the login email and API token |
| `jql` or `jql_env` | The JQL search text itself, or the name of a variable that holds it. Exactly one is required |
| `issue_limit`, `updated_lookback` | Most issues per claim, and how far back to look for changes (for example `24h`) |
| `changelog_limit`, `remote_link_limit` | Most history items and remote links to read per issue |
| `metadata_limit` | Most site-detail records to read |
| `allow_empty_successor` | Leave it `false`; `true` is refused for now, as explained below |
| `limits.*` | Size and time caps for one claim, covered in the limits section |

`allow_empty_successor: true` would mean "an empty or much smaller
result is correct; let it replace the old one." This build can't send
that promise yet, so it refuses the setting with failure class
`unsupported_option` rather than pretending.

## Credentials stay out of the file

Secrets never go in the config file. `email_env`, `token_env`, and
`jql_env` name environment variables the worker process already has.
So `token_env: JIRA_API_TOKEN` means "read the token from the variable
called `JIRA_API_TOKEN`." Each value has to be a plain variable name:
letters, digits, and underscores, at most 128 bytes. That's checked in
`config.go`, and the same applies to any `*_env` name you use here.
Secret values never appear in facts, source addresses, logs, or status
messages, and error messages name the config key, never the value.

## Time and size limits

Eshu gives each claim a deadline (the first lease expiry; heartbeat
messages don't extend it) and stops the process there. The default
lease is 60 seconds, set with `ESHU_COMPONENT_COLLECTOR_CLAIM_LEASE_TTL`
on the worker. A large site needs more, and the reference setup uses
5 minutes with a 30-second heartbeat. The collector stops starting new
Jira calls shortly before the deadline (the smaller of 2 seconds or 5%
of the time left), cancels the call in progress, and returns
`retryable` with failure class `claim_deadline_exceeded` and no facts.
Dropping finished work there is deliberate: it stops a half-finished
read from being stored as complete. With no deadline,
`limits.claimTimeoutSeconds` (default 300) is the only time bound, and
an already-passed deadline fails at once, before any Jira call.

Size caps live under `config.limits`: `maxRecordsPerClaim`,
`maxPayloadBytes` for one fact, and `maxResultBytes` for the whole
result. A large result can pass the host's default 4 MiB output cap, so
raise `process.stdout_limit_bytes` and `limits.maxResultBytes`
together. The collector fails with `result_budget_exceeded` rather
than let the host drop an oversized result.

## Switching from the built-in collector without re-keying everything

| Setup value | Rule |
| --- | --- |
| `host.scope.id` | Must equal the old target's scope ID, or every stored key changes |
| `host.scope.kind` | Must be `jira_site` |
| `host.sourceSystem` | Must be `jira`, or the source label changes on every fact |

The activation instance ID (`--instance`) must be new and different
from the built-in collector's instance ID. Read the real values from
the live built-in setup, never copy the placeholder
(`jira:site:example`), and run only one of the two collectors per
site.

## Versions it builds against

These are exact, straight from `go.mod`. Please don't substitute
another SDK line. Note where the SDK lives: both modules are
`github.com/eshu-hq/eshu/sdk/go/*` at `v0.2.0`, so they come from the
Eshu core repo, not from `eshu-sdk`.

| Module | Version |
| --- | --- |
| Go | `1.26.11` |
| `github.com/eshu-hq/eshu/sdk/go/collector` | `v0.2.0` |
| `github.com/eshu-hq/eshu/sdk/go/factschema` | `v0.2.0` |
| `gopkg.in/yaml.v3` | `v3.0.1` |

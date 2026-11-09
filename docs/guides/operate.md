# Operate

What you'll actually see once claims are running, and what every
ending means. Run states (`complete`, `partial`, `retryable`,
`terminal`) are explained in [concepts](../concepts/README.md).

## What you'll actually see

The main signal is the failure class on the work item. On current Eshu
`main`, the worker also records each result as
`eshu_dp_component_extension_*` metrics and one
`component extension result` log line (including source wait time and
fact counts), and it honours `retry_after_seconds`, the wait the
collector asks for before a retry, capped at one hour.

The collector also writes one JSON line per claim to stderr with the
failed step, HTTP status, and counts. The host keeps only the byte
count and drops the text, so that line only helps in a local run
(`collector --sdk-stdio < request.json`) or in tests. This collector
emits no metrics or spans of its own: the manifest declares the prefix
`eshu_dp_jira_collector_`, but nothing emits under it yet.

## When a claim fails

`retryable` means the host can try again later. `terminal` means it
should stop until a person fixes something. This collector has no
`provider-rejected` or `internal-error` class of its own (those names
belong to the Confluence collector); its vocabulary comes from
`failure.go`, `client_errors.go`, and `internal/httpx/failure.go`, and
every class below is in one of those files.

Terminal classes, so retrying won't help on its own:

| Class | What happened |
| --- | --- |
| `invalid_config` | The activation config can't run, including an unresolved or badly named `*_env` value |
| `unsupported_option` | A recognised option the pinned SDK can't honour, today that's `allow_empty_successor: true` |
| `invalid_evidence` | Provider data that can't become a fact, such as an issue with no id or key |
| `permission_hidden` | Jira refused on permission or issue-security grounds (HTTP 401 or 403 under the Jira status policy) |
| `deleted` | The issue or site is missing (HTTP 404 under that policy) |
| `archived` | The project or issue is archived (HTTP 410, or the archived sentinel with no distinct status) |
| `terminal` | Any other non-retryable provider status (other 4xx), malformed input, or a response past the size cap |
| `record_budget_exceeded` | The claim went past its fact-count bound |
| `payload_budget_exceeded` | One fact went past its payload-size bound |
| `result_budget_exceeded` | The encoded result went past its size bound |

Retryable classes, so a later attempt may well succeed:

| Class | What happened |
| --- | --- |
| `claim_deadline_exceeded` | The claim's lease deadline passed; the result carries no facts |
| `rate_limited` | Jira throttled the collector (408, 409, 425, or 429 under the shared status policy) |
| `retryable` | A transient transport or provider failure, including 5xx responses |

A couple of shared helpers define `auth_denied` and `not_found` as
generic fallbacks, but Jira's own status policy maps those statuses
to `permission_hidden` and `deleted` instead, so they're not what a
Jira claim reports.

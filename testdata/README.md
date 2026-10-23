# Test data

## `source/` — recorded Jira sites

Each `source/<scenario>.json` is one fake Jira Cloud site: the activation
`config`, the credential `env`, a fixed clock (`now`), and the HTTP responses
keyed by `METHOD path?sortedQuery` (a POST search adds `#<nextPageToken>`).
They drive the extracted collector with no network (`replay_test.go`) and the
in-tree collector when the goldens are regenerated. Every value is synthetic:
the host is `jira.fixture.invalid` and the token is a placeholder.

`source/generate.py` writes the scenarios. From the repository root:

```bash
python3 -I testdata/source/generate.py
```

| Scenario | What it exercises |
| --- | --- |
| `basic` | two-page token search, changelog with redacted fields and a duplicate-key pair, remote links (GitHub PR, GitLab MR, Confluence, duplicates, an anonymous link, a credentialed URL), all five metadata kinds |
| `degraded` | metadata the site hides (403), does not support (404), or archived (410) becoming warning facts |
| `paged` | issue cap with more pages available, changelog and metadata pagination, remote-link cap |
| `failure_*` | 400, 401, 403, 404, 410, 429 with `Retry-After`, 503, a changelog 500, a remote-link 410, a metadata 502, and an undecodable search body |

## `golden/` — what the in-tree collector produced

`golden/<scenario>.golden.json` holds the in-tree collector's output for the
same responses: scope, generation (id, observed time, freshness hint), and per
fact the kind, stable key, schema version, source confidence, source ref, and
payload, in emission order; or, for failures, the class, terminality,
operation token, and retry guidance. `parity_test.go` replays each scenario
through this repository's handler and compares them.

Reference: eshu-hq/eshu `origin/main` at `ce050339cf` (2026-10-23).

### Reproduce

The generator is `golden/intree_golden_generator.go.txt`, a throwaway test for
the in-tree package. Never commit it to eshu.

```bash
ESHU=/path/to/eshu               # any clone of eshu-hq/eshu
HERE=$(pwd)                      # this repository
git -C "$ESHU" fetch origin
git -C "$ESHU" worktree add -b drive/8858-golden /tmp/eshu-golden ce050339cf
cp "$HERE/testdata/golden/intree_golden_generator.go.txt" \
   /tmp/eshu-golden/go/internal/collector/jira/zz_golden_parity_test.go
(cd /tmp/eshu-golden/go && \
  GOLDEN_SCENARIOS="$HERE/testdata/source" GOLDEN_OUT_DIR="$HERE/testdata/golden" \
  go test ./internal/collector/jira -run TestZZGoldenParity -count=1)
git -C "$ESHU" worktree remove --force /tmp/eshu-golden
```

Change the commit to move the reference. A moved reference that changes a
golden is a behavior change in the in-tree collector, not noise: read the diff.

The scenarios use a ten-year `updated_lookback`. The in-tree collector decides
whether a window is stale against the real clock while the fixtures use a fixed
one; the long lookback keeps that check false so the recorded freshness hints
match. `TestFixtureClockStaysInsideTheInTreeStaleWindow` fails with a pointer
here before the window closes.

## Host round trip

`golden/host_roundtrip_test.go.txt` is a second throwaway test for the eshu
worktree. It starts a fake Jira over real HTTP from one scenario, runs the real
extension host (`NewSource`, `ProcessRunner`, result validation, payload-schema
check, producer-grant recheck, envelope adapter) against the built collector
binary, runs the in-tree collector against the same server, and compares the
committed envelopes: kind, stable key, schema version, confidence, collector
kind, scope, generation, fencing token, source ref, and payload. Fact IDs must
differ. For failure scenarios it compares the host's failure class and
terminality with the in-tree collector's.

```bash
(cd "$HERE" && CGO_ENABLED=0 go build -trimpath -o /tmp/jira-collector ./cmd/collector)
cp "$HERE/testdata/golden/host_roundtrip_test.go.txt" \
   /tmp/eshu-golden/go/internal/collector/extensionhost/zz_host_roundtrip_test.go
(cd /tmp/eshu-golden/go && JIRA_COLLECTOR_BIN=/tmp/jira-collector \
  JIRA_SCENARIO="$HERE/testdata/source/basic.json" JIRA_MANIFEST="$HERE/manifest.yaml" \
  go test ./internal/collector/extensionhost -run TestZZHostRoundTrip -count=1 -v)
```

Result at reference `ce050339cf`: all fifteen scenarios pass. The four
successful runs compare 22, 22, 10, and 19 unique facts; the eleven failure
scenarios agree on class and terminality. The freshness hints differ here only
because the real collector reads the wall clock for its window.

## `cassettes/supply-chain-demo.json`

Copied from eshu `testdata/cassettes/jira/supply-chain-demo.json`. It is a
facts replay (already-emitted payloads), not an HTTP recording, so it cannot
drive the collector. `parity_test.go` uses it only to pin the
`work_item.record` payload key set.

## Intentional differences from the in-tree collector

Parity is judged on kind, stable key, payload, scope, generation, and the
freshness hint. These differ by design or are not comparable:

1. **Fact IDs, fencing token, and partition key** are host-owned. The host
   derives them from the component and claim, so they differ.
2. **Duplicate stable keys** collapse in the handler (last write wins, at the
   first position). The in-tree store resolves a repeated fact id the same way
   at write time, and the extension host rejects a conflicting duplicate, so
   the handler does it first. The golden is compared after the same collapse.
3. **Result state.** A claim whose metadata includes warning facts returns
   `partial` (the host commits it like `complete` and records the state).
4. **Trusted deletion.** `allow_empty_successor` cannot be honored: SDK
   v0.2.0's `Generation` has no `trusted_deletion` field. The config key is
   rejected when true (terminal `unsupported_option`) instead of silently ignored.
5. **Response size cap.** Provider responses over 32 MiB fail; the in-tree
   helper reads without a bound.
6. **Issues without an id or key** fail terminal (`invalid_evidence`) at
   search time; in-tree they reach the changelog call with a malformed path.
7. **Unresolvable configuration** is a terminal result (`invalid_config`);
   in-tree it stops the process at start.
8. **Failure operation tokens** (`search_issues`, `read_changelog`, ...), the
   config rejection reason, the HTTP status, and the collection counters go to
   one JSON line on stderr. The SDK status has no field for them, and the
   host's process runner keeps stderr only as a byte count, so an operator does
   not see that line. The in-tree fetch span and counters have no equivalent,
   and the manifest's `metricsPrefix` is declared but nothing emits it. See the
   README section "Operator signals" and eshu-hq/eshu#8908.
9. **Claim deadline.** The in-tree worker owned its lease; here the host
   kills the process at the initial lease expiry. The handler stops starting
   provider calls at the deadline minus min(2s, 5% of the time left) and ends
   `retryable` with `claim_deadline_exceeded` and no facts (never `partial`).
10. **Retry guidance.** A retryable result must carry `retry_after_seconds`; a
   missing provider `Retry-After` becomes 60 s for rate limits and 30 s
   otherwise, and provider values are capped at 24 h.

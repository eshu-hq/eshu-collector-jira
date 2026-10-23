# eshu-collector-jira

Eshu Jira work-item collector, extracted from the Eshu core repository.

**Pre-release. Holds no producer authority until its reviewed cutover
(eshu-hq/eshu#8858).**

This repository runs through the Eshu component-extension host with the
`process` runner. The host passes one bounded JSON request on stdin; the
collector writes one `collector-sdk/v1alpha1` result on stdout. It receives no
Postgres, graph, queue, lease, or workflow handle, and it never imports
`github.com/eshu-hq/eshu/go/internal/...` (`boundary_test.go` enforces this).

It builds against the published SDK modules, with no `replace` directives:

- `github.com/eshu-hq/eshu/sdk/go/collector v0.2.0`
- `github.com/eshu-hq/eshu/sdk/go/factschema v0.2.0`

Wire protocol: `collector-sdk/v1alpha1`.

## Build and test

```bash
go build ./...
go vet ./...
go test -race -count=1 ./...
```

`scripts/run-conformance.sh` runs the gate that CI runs
(`.github/workflows/ci.yml`; the `ci-complete` job aggregates the others).

## Least-privilege credentials

Source credentials stay in the collector runtime environment, named by
environment variables in the activation config, and never enter fact payloads,
source URIs, logs, or statuses.

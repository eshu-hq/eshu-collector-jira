# Diagrams

Every diagram for this collector lives on this page, so there's one
place to keep them honest. Other pages link here instead of copying
the picture.

## System context

How a source system, this collector, the Eshu host, and Eshu core
fit together. The collector only talks to its source and to the
host over stdin/stdout.

```mermaid
flowchart LR
  A["Jira Cloud site"] --> B["This collector"] --> C["Eshu host process"] --> D["Eshu core"]
```

Text version: Jira Cloud site, then this collector, then the Eshu host
process (checks and stores the result), then Eshu core. The collector
never talks to Eshu's database directly.

## Claim lifecycle

What happens to one claim, from the host sending it on stdin to the
host committing the result or recording a failure class. The
collector checks config and credentials before any provider call.

```mermaid
sequenceDiagram
  participant H as Eshu host
  participant C as This collector
  participant J as Jira Cloud
  H->>C: one claim on stdin
  C->>C: check config and resolve credentials
  C->>J: bounded reads for issues, history, links, site details
  J-->>C: responses
  C->>C: redact, build facts, check budgets
  C-->>H: one result on stdout
  H->>H: validate and commit, or record the failure class
```

Text version: the host sends one claim, the collector checks its config
and credentials, reads Jira within its limits, redacts and builds
facts, and returns one result. The host then validates and commits it,
or records why it failed.

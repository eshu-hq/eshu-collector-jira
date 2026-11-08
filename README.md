# eshu-collector-jira

Status: pre-release. Please don't rely on it in production yet.

In a nutshell: this little program reads work items from one Jira Cloud
site and hands Eshu a checked, redacted summary of what it found. Eshu is
the core system, and a collector is just a helper that reads one outside
system and turns what it sees into facts, which are small typed records.
This one only ever reads Jira. It never writes to Jira.

```mermaid
flowchart LR
  A["Jira Cloud site"] --> B["This collector"] --> C["Eshu host process"] --> D["Eshu core"]
```

Text version: Jira Cloud site, then this collector, then the Eshu host
process (it checks and stores the result), then Eshu core. The collector
never talks to Eshu's database directly.

## Is this the one you want?

Reach for it when Eshu needs current Jira issues, their change history,
remote links, and basic site details for one Jira Cloud site, refreshed
on a schedule Eshu controls.

Skip it if you want to change anything in Jira, read more than one site
from one setup (just run one setup per site), or use it as a general
Jira API tool. It reads only what the fact list in
[how it works](docs/concepts/README.md) covers, and that's it.

## How Eshu runs it

Eshu's component host starts the program for one claim (one job) at a time.
It sends one bounded `collector-sdk/v1alpha1` request on stdin and reads
one result back from stdout: `collector --sdk-stdio`.
The collector gets no database, graph, queue, or lease handle of its own.
The host builds the final fact envelope, checks the result, and owns
claims and commits, so this program never stores anything itself.

## Documentation

| Page | What's in it |
| --- | --- |
| [Documentation](docs/README.md) | Start here: the whole docs tree in one index |
| [Getting started](docs/getting-started/README.md) | The fastest honest path to a first successful claim |
| [Concepts](docs/concepts/README.md) | What it does, the facts story, identity and grants, and what it never does |
| [Configuration](docs/reference/configuration.md) | Every setting, credentials, limits, and identity on a switch |
| [Facts](docs/reference/facts.md) | The fact kinds, schemas, and confidence, mirroring the manifest |
| [Guides](docs/guides/README.md) | Seed and enable, operate, troubleshoot, release |
| [Diagrams](docs/diagrams/README.md) | Every diagram, each with a text version |
| [Example config](config.example.yaml) | A full activation config you can copy and adapt |
| [Manifest](manifest.yaml) | Component identity, fact list, and the core version range |
| [Grant requests](grant.example.json) | The nine producer grants this component asks core for |
| [Contributing](CONTRIBUTING.md) | The local check to run before you push |

## Related repos

These are neighbours, not extra build dependencies beyond what's pinned
in `go.mod` (see [configuration](docs/reference/configuration.md) for the exact
pins). The SDK modules this build actually uses,
`github.com/eshu-hq/eshu/sdk/go/collector` and
`github.com/eshu-hq/eshu/sdk/go/factschema` at `v0.2.0`, live in the
Eshu core repo, not in a separate SDK repo.

- [Eshu core](https://github.com/eshu-hq/eshu) runs this collector and
  holds those SDK modules.
- [eshu-deploy](https://github.com/eshu-hq/eshu-deploy) has deployment
  examples. It's related reading only; nothing here depends on it.

## Development

Run `make ci` and you've run the same lint, test, and race checks CI runs.

## Contributing, security, and license

Contributing notes live in [CONTRIBUTING.md](CONTRIBUTING.md). There's
no separate security policy file yet, so please don't open a public
issue for a suspected vulnerability or a credential leak; tell the
maintainers privately instead. The license is MIT, in
[LICENSE](LICENSE).

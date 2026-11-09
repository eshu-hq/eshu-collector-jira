# Facts reference: Jira collector

This table mirrors [manifest.yaml](../../manifest.yaml). The manifest
is the source of truth, so if you change one you need to update the
other in the same change.

| Fact kind | Schema | Confidence | One fact is |
| --- | --- | --- | --- |
| `work_item.record` | `1.0.0` | `reported` | one issue |
| `work_item.transition` | `1.0.0` | `reported` | one status change in an issue's history |
| `work_item.external_link` | `1.0.0` | `reported` | one remote link on an issue |
| `work_item.project_metadata` | `1.0.0` | `reported` | one project on the site |
| `work_item.issue_type_metadata` | `1.0.0` | `reported` | one issue type on the site |
| `work_item.status_metadata` | `1.0.0` | `reported` | one status on the site |
| `work_item.workflow_metadata` | `1.0.0` | `reported` | one workflow on the site |
| `work_item.field_metadata` | `1.0.0` | `reported` | one field on the site |
| `work_item.metadata_warning` | `1.0.0` | `reported` | site details the collector couldn't read |

The component behind that table is `dev.eshu.collector.jira`, version
`0.1.0`, collector kind `jira`. It declares support for Eshu core
`>=0.0.5 <0.2.0` and speaks `collector-sdk/v1alpha1` over the process
adapter. It's worth checking that range against your core version
before you enable it, because a mismatch makes the host refuse the
component.

Every kind above belongs to Eshu core, so the host won't accept it
without a producer grant. The nine requests live in
[grant.example.json](../../grant.example.json); see
[concepts](../concepts/README.md) for how grants and identity work,
and [seed and enable](../guides/seed-and-enable.md) for recording
them.

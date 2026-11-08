# Getting started

Status: pre-release. Please don't rely on this in production yet.

The honest first thing to know: you don't run this collector as a
daemon. Eshu's host starts it for one claim (one job) at a time, sends
one bounded request on stdin, and reads one result from stdout. So
"getting started" really means getting one activation enabled and one
claim to succeed.

1. Check your core version fits the range in the [manifest](../../manifest.yaml)
   (`>=0.0.5 <0.2.0`). A mismatch makes the host refuse the component.
2. Copy [config.example.yaml](../../config.example.yaml). Set `site_id` and
   `base_url` for your site, exactly one of `jql` / `jql_env`, and the
   `email_env` / `token_env` names (for example `JIRA_EMAIL` and
   `JIRA_API_TOKEN`). Secrets stay in those environment variables, never
   in the file. Full detail is in the
   [configuration reference](../reference/configuration.md).
3. Record the nine producer grants from
   [grant.example.json](../../grant.example.json) (see
   [seed and enable](../guides/seed-and-enable.md)).
4. Seed the image and enable one activation per site. If you're replacing
   the built-in collector, keep `host.scope.id`, `host.scope.kind`
   (`jira_site`), and `host.sourceSystem` (`jira`) identical, and pick a
   new instance ID.
5. Let Eshu run a first claim. You're looking for a `complete` (or
   honestly `partial`) result, not a failure class. On a large site, give
   the worker lease at least 5 minutes or you'll meet
   `claim_deadline_exceeded` first.

Where to go next: [concepts](../concepts/README.md) for the mental
model, the [configuration reference](../reference/configuration.md)
for every setting, [facts](../reference/facts.md) for what comes out,
and [troubleshoot](../guides/troubleshoot.md) when the first claim
doesn't land.

# Seed and enable

From a published image to a live activation, in order.

## The image, and seeding it into place

The published image is `ghcr.io/eshu-hq/eshu-collector-jira`. It holds
one static binary with no shell, and the binary can install itself
into a component folder, which is how a startup (init) container sets
it up:

```bash
collector seed --dest /component [--image-ref ghcr.io/eshu-hq/eshu-collector-jira@sha256:<digest>]
```

That writes `/component/collector` (mode 0755) and
`/component/manifest.yaml`. `--image-ref` (or the `COLLECTOR_IMAGE_REF`
environment variable; the flag wins) stamps the exact deployed image
reference into the manifest, because an image can't contain its own
digest. The manifest in this repo keeps an all-zero placeholder digest
on purpose. A bad image reference fails before any file is written,
and the error never echoes the value. `scripts/pin-digest.sh <repo:tag>`
only prints the `repo@sha256:...` reference of a pushed image; it
edits nothing.

## Then record grants and enable

The host refuses this component's facts without a producer grant for
each kind. The nine requests are in
[grant.example.json](../../grant.example.json), tied to `dev.eshu.collector.jira`
and its manifest version. Record them before you enable (see
[concepts](../concepts/README.md) for why grants exist), enable one
activation per scope from your copy of
[config.example.yaml](../../config.example.yaml), then let Eshu run a
first claim and check it with [operate](operate.md).

If you're replacing Eshu's built-in collector, do the identity checks
in the [configuration reference](../reference/configuration.md) first.
Getting those wrong re-keys stored data, and that's much easier to
prevent than to undo.

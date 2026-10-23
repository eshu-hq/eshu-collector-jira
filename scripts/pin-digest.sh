#!/usr/bin/env bash
# Resolves the immutable digest of the pushed collector image and prints the
# manifest artifact line to pin. With --write it also rewrites the artifact
# line in manifest.yaml (and nothing else).
#
#   scripts/pin-digest.sh ghcr.io/eshu-hq/eshu-collector-jira:0.1.0 [--write]
#
# After the first real digest lands, set placeholderDigestAllowed to false in
# manifest_test.go so the gate fails on an unpinned manifest. The image cannot
# carry its own digest, so a deployment stamps the reference it actually pulled
# with `collector seed --image <repo>@sha256:<digest>`.
set -euo pipefail
ref="${1:?usage: pin-digest.sh <repo:tag> [--write]}"
write="${2:-}"
cd "$(dirname "$0")/.."
digest=""
if digest="$(docker buildx imagetools inspect "$ref" --format '{{.Manifest.Digest}}' 2>/dev/null)" && [ -n "$digest" ]; then
  :
else
  digest="$(docker inspect --format='{{index .RepoDigests 0}}' "$ref" | sed 's/^.*@//')"
fi
case "$digest" in
  sha256:????????????????????????????????????????????????????????????????) ;;
  *) echo "pin-digest: could not resolve a sha256 digest for $ref" >&2; exit 1 ;;
esac
repo="${ref%%:*}"
repo="${repo%%@*}"
image="${repo}@${digest}"
echo "Pin this in manifest.yaml artifacts:"
echo "  image: ${image}"
if [ "$write" = "--write" ]; then
  tmp="$(mktemp)"
  sed -E "s#^([[:space:]]*image:[[:space:]]*)[^[:space:]]+#\1${image}#" manifest.yaml > "$tmp"
  mv "$tmp" manifest.yaml
  echo "manifest.yaml updated"
fi

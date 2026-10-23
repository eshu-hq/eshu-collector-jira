#!/usr/bin/env bash
# Resolves the immutable digest of a pushed collector image and prints the
# reference to copy into a deployment. It never edits a file.
#
#   scripts/pin-digest.sh ghcr.io/eshu-hq/eshu-collector-jira:v0.1.0
#
# manifest.yaml keeps the all-zero placeholder digest permanently: it is
# go:embed'ed into the binary, so a digest written into it would describe an
# older build than the image that carries it. A deployment stamps the digest it
# actually pulled when it seeds the component home:
#
#   collector seed --dest /component --image-ref <repo>@sha256:<digest>
#   (or COLLECTOR_IMAGE_REF)
set -euo pipefail
ref="${1:?usage: pin-digest.sh <repo:tag>}"
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
# Drop a digest or a tag, keeping a registry port ("host:5000/name:tag").
repo="${ref%%@*}"
case "${repo##*/}" in
  *:*) repo="${repo%:*}" ;;
esac
echo "Use this reference for the deployment (collector seed --image-ref, or seed.image):"
echo "  ${repo}@${digest}"

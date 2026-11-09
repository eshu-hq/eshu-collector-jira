# Release

## Building, testing, and releasing

```bash
go build ./... && go vet ./...
go test -race -count=1 ./...
make ci   # lint + test + race; the full local check CI runs
```

`scripts/run-conformance.sh` runs the same gate as CI
(`.github/workflows/ci.yml`, reported as `go-ci / ci-complete`).
Releases run from `.github/workflows/release.yml` on every push to
`main` and on version tags (`vMAJOR.MINOR.PATCH`, with an optional
pre-release suffix). Nothing runs for a pull request, and the workflow
deploys nothing. A `main` push publishes the moving `main`, `latest`,
and `sha-<full-sha>` image tags, so use moving tags only where
floating is intended and pin real deployments to digests. A version
tag has to point at the protected `main` head and match
`metadata.version` in `manifest.yaml`, or nothing is published. The
workflow builds `linux/amd64`, pushes the image, writes the digest to
the job summary, signs it with cosign (keyless), and attaches
build-provenance and SBOM attestations, using no secret beyond
`GITHUB_TOKEN`.

After a release, copy the
`ghcr.io/eshu-hq/eshu-collector-jira@sha256:<digest>` reference from
the job summary into the deployment and check it before use:

```bash
cosign verify ghcr.io/eshu-hq/eshu-collector-jira@sha256:<digest> --certificate-identity-regexp '^https://github.com/eshu-hq/eshu-collector-jira/\.github/workflows/release\.yml@refs/tags/v' --certificate-oidc-issuer https://token.actions.githubusercontent.com
gh attestation verify oci://ghcr.io/eshu-hq/eshu-collector-jira@sha256:<digest> -R eshu-hq/eshu-collector-jira
```

One last chore after the first release: set the GHCR package to
public, because GitHub creates packages as private and a cluster can't
pull a private package without credentials. It's also worth
restricting who can create `v*` tags to maintainers.

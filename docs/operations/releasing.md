# Cut a release

Pushing a tag `vMAJOR.MINOR.PATCH` (or `vMAJOR.MINOR.PATCH-PRERELEASE`) runs `.github/workflows/release.yml`, which builds and publishes the container image, the binary archives and a GitHub release. The pipeline's design (what is built where, the tags, the archive layout) is proposed, not yet confirmed.

## Before you tag

1. On `develop`, run `make verify-release` and `make vuln`. `verify-release` is the full profile followed by the [upgrade and recovery compatibility matrix](upgrade-compatibility.md): every earlier schema version upgraded and rebuilt from its archive by the binary you are about to tag. When the release adds a migration or changes the DuckDB driver, update the versions and the results table in that page.
2. Check that the Release workflow run for the commit you tag, `develop`'s HEAD, passed. Every push to `develop` runs every build step and publishes nothing, as does a pull request that changes the workflow, `Dockerfile`, `.dockerignore`, `Makefile`, `go.mod`, `go.sum` or `scripts/release-archive.sh`. Without such a run, start one with **Actions > Release > Run workflow** on `develop`.

The compatibility matrix is a manual step here, not a job in the release workflow. That workflow builds and publishes from the tag and runs no tests; the Go tests run in CI on every push to `develop`, which already includes the matrix's seed versions. The whole matrix needs a quiet machine for its DuckDB fixtures, and a failure has to stop the tag before it is pushed, not after the image job. This choice is proposed, not yet confirmed.

## Tag

```bash
git switch develop && git pull --ff-only
git tag -a v0.2.0 -m "nats-lens v0.2.0"
git push origin v0.2.0
```

A tag that is not a semantic version `v<major>.<minor>.<patch>[-<prerelease>]` stops the run before it builds: no leading zeros (`v01.2.3`), no empty prerelease identifiers (`v1.2.3-rc..1`), no build metadata (`+build`). A tag with a `-` part, such as `v0.2.0-rc.1`, makes a prerelease.

Tag runs queue one at a time. GitHub keeps only one waiting run: a third tag pushed while one run builds and another waits cancels the waiting one. Re-run a cancelled run from the Actions page.

## First release

GHCR creates the package `nats-lens` private on its first push, so nobody else can pull the first image. After the first tag's image job has pushed:

1. Open the package (the repository's **Packages** list, then `nats-lens`), then **Package settings > Change visibility > Public**.
2. Check that it pulls without credentials:

   ```bash
   docker logout ghcr.io
   docker pull ghcr.io/shadone/nats-lens:v0.1.0
   ```

Later releases push to the same package and stay public.

## What gets published

| What | Where |
| --- | --- |
| Container image, linux/amd64 and linux/arm64 | `ghcr.io/shadone/nats-lens:v0.2.0`, `:0.2`, and `:latest` for a release that is not a prerelease and has the highest version. A prerelease gets only `:v0.2.0-rc.1`. |
| Binary archives | `natslens_0.2.0_<os>_<arch>.tar.gz` for linux/amd64, linux/arm64, darwin/arm64 and darwin/amd64, attached to the GitHub release. Each holds a directory of the same name with `natslens`, `LICENSE`, `NOTICE` and `README.md`. |
| `checksums.txt` | SHA-256 of every archive, attached to the release. |
| Build provenance | A GitHub attestation for every archive and for the image. |
| GitHub release | Titled with the tag, with install notes followed by notes generated from the merged pull requests. |

Every binary prints the tag: `natslens version` shows `natslens v0.2.0 (go1.27.3 linux/amd64)`. A build from a push to `develop`, a pull request or a manual run is stamped `dev-<commit>`.

A release that is not a prerelease becomes `:latest` and the GitHub release marked Latest only when no published release has a higher version, so a patch for an older line (v0.1.3 after v0.2.0) leaves both on the newer line.

## How each part is built

- **The web UI** is built once per run by the Dockerfile's `web` stage (`--target webui`) and embedded in the macOS binaries. The image and the Linux binaries build it with the same stage.
- **Linux binaries** are the Dockerfile's `binary` stage: the image's own build, in Debian 12 with Go cross-compiling and Debian's gcc cross toolchain for arm64. They link glibc and libstdc++ dynamically and need glibc 2.34 or later and libstdc++ from GCC 12 or later (Debian 12, Ubuntu 22.04, RHEL 10 or newer). Alpine (musl) is not supported.
- **macOS binaries** are built natively on GitHub's `macos-15` (arm64) and `macos-15-intel` (amd64) runners with `make build-release`, using the Go release of the Dockerfile's `golang` image, for macOS 13 or later (`MACOSX_DEPLOYMENT_TARGET`). They are not signed with a Developer ID or notarized: a file downloaded with a browser needs `xattr -d com.apple.quarantine natslens` before macOS runs it.
- **The image** is built for both platforms by `docker/build-push-action` after the binaries succeed, so a failed build publishes nothing. Its labels and index annotations carry the source repository, the description and the license, `AGPL-3.0-only`. Only a tag run has the job with permission to push and attest; other runs build the image in a job that can only read.

No job reads a cache from an earlier run, so a release is built from the tagged sources only.

## Verify a release

```bash
docker run --rm ghcr.io/shadone/nats-lens:v0.2.0 version
docker run --rm -p 127.0.0.1:8080:8080 ghcr.io/shadone/nats-lens:v0.2.0 demo   # open http://127.0.0.1:8080
gh attestation verify oci://ghcr.io/shadone/nats-lens:v0.2.0 --repo shadone/nats-lens

gh release download v0.2.0 --repo shadone/nats-lens --pattern 'natslens_0.2.0_linux_amd64.tar.gz' --pattern checksums.txt
sha256sum -c checksums.txt --ignore-missing
gh attestation verify natslens_0.2.0_linux_amd64.tar.gz --repo shadone/nats-lens
```

## Build a release locally

```bash
make web
make build-release VERSION=v0.2.0-local BIN=out/natslens
scripts/release-archive.sh v0.2.0-local "$(go env GOOS)" "$(go env GOARCH)" out/natslens dist

docker buildx build --platform linux/arm64 --target binary --build-arg VERSION=v0.2.0-local --output type=local,dest=out/linux .
docker buildx build --platform linux/arm64 --build-arg VERSION=v0.2.0-local --load -t natslens:local .
```

A multi-platform build needs a `docker-container` builder (`docker buildx create --use`). Two platforms at once need more than the 2 GiB of memory a default colima VM has; build one platform at a time there.

## If a run fails

- **Before the image job:** nothing is published. Fix the cause on `develop`, delete the tag (`git push origin :refs/tags/v0.2.0`, `git tag -d v0.2.0`) and tag again.
- **In or after the image job:** the image may already be pushed. Re-run the failed jobs from the Actions page; pushing the image again for the same tag replaces it. `gh release create` refuses a tag that already has a release, so if the release job failed after creating one, delete it first with `gh release delete v0.2.0 --repo shadone/nats-lens`.

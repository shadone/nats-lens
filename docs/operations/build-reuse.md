# Build the binary with verified production assets

`make build` verifies production UI inputs and output bytes, rebuilding the UI only when they differ. It then builds the Go binary while holding the same per-worktree asset lock. Use it for a binary you intend to install.

The content-verification and cache partitioning policy is proposed, not yet confirmed. Container execution validation remains pending until a Docker builder is available.

## Choose a workflow

| Command | Artifact guarantee |
| --- | --- |
| `make build` | Verifies/rebuilds production assets, then embeds them in `bin/natslens`. |
| `make build-full` | Forces TypeScript checking and Vite generation, then embeds the result. |
| `make web` | Verifies/rebuilds production assets without compiling Go. |
| `make web-full` | Forces production asset generation without compiling Go. |
| `make build-go` | Embeds whatever assets already exist. It does not verify freshness; without `dist/index.html`, the binary serves the placeholder. |
| `make web-dev` | Serves source through Vite with hot reload; it does not update the embedded production assets. |
| `cd web && npm run build` | Explicit standalone frontend rebuild; it does not update the local verification manifest. |

The dependency prerequisite still runs `npm ci` when package.json, package-lock.json or .npmrc is newer than the installed marker. Asset reuse assumes an unmodified npm-installed dependency tree. If you edit installed dependencies or use development links, force a frontend rebuild or reinstall first; the verifier does not hash the entire node_modules tree.

## What invalidates production assets

The verifier hashes web inputs (including source, public files, configuration, package/lock files and generated TypeScript), proto inputs, the installed dependency marker, its own implementation, Node version and build environment. Shell/make bookkeeping variables are excluded. Environment values are hashed, not written to the manifest.

It also verifies every existing output byte. Deleted or renamed inputs, missing or altered outputs, a missing index, and corrupt verification state cause a rebuild. A Go-only source edit does not invalidate the UI. Changing the build environment can conservatively invalidate it even when that environment change would not alter emitted code.

TypeScript and Vite write to a private staging directory. The previous UI stays available if generation fails or inputs change during generation. On success the verified directory is published, and the small state file is replaced. Do not edit source files or run standalone Vite production builds concurrently with a production build; the lock coordinates wrapper-based asset generation and Go embedding, not arbitrary filesystem writers.

State and staging live under ignored `internal/webui/.build-*` paths, outside the embedded dist tree and outside Docker's build context. A normal successful build leaves a digest manifest, not extra copies of the UI. A rebuild temporarily needs space for both the old and staged assets.

## Recover an interrupted local build

The lock's `owner.json` records its PID. Ordinary errors clean up the lock and staging. If a process was killed before cleanup, verify that its PID is no longer running before removing that worktree's `.build-lock` directory. Never remove another active build's lock or clear unrelated caches.

`make web-full` bypasses a valid asset-cache hit; it still uses the same lock and publication checks. `make clean` retains its existing behavior of removing bin only.

## Container caches and remaining gate

The pinned Go/base image is unchanged. The Dockerfile mounts the module cache for both module download and compilation. Compilation caches are separated by Go version, pinned base-image prefix, build architecture, target architecture and a cgo cache epoch. Bump the epoch if refreshing the native compiler/header setup without a base-image change. Module content is shared; compilation access to one partition is locked.

This follows Docker's [cache-mount guidance](https://docs.docker.com/build/cache/optimize/#use-cache-mounts) and [build-argument scoping](https://docs.docker.com/build/building/variables/#scoping). Cache contents are a performance aid, not a replacement for pinned module and image inputs.

The local daemon was unavailable during implementation. Neither target architecture is newly certified by the cache changes. When a builder is available, run both builds, inspect their architecture and execute the binaries in a compatible runtime:

```sh
docker buildx build --platform linux/amd64 --load -t natslens:cache-check-amd64 .
docker image inspect natslens:cache-check-amd64 --format '{{.Architecture}}'
docker run --rm --network none natslens:cache-check-amd64 checks list

docker buildx build --platform linux/arm64 --load -t natslens:cache-check-arm64 .
docker image inspect natslens:cache-check-arm64 --format '{{.Architecture}}'
docker run --rm --network none natslens:cache-check-arm64 checks list
```

Repeat after a Go source edit and inspect cache reuse with `docker buildx du`. Running a non-native image may require an emulator; a successful cross-build alone is not an execution test. Do not prune a shared builder to measure a cold cache.

See the [local before/after measurements](../research/build-reuse-2026-11-08.md). Browser bundle size and transfer compression are separate work.

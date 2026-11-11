# Development

This page is for contributors: what to install, how to build and run nats-lens against a local NATS cluster, and the make targets. How to propose a change is in [CONTRIBUTING.md](../CONTRIBUTING.md).

## Prerequisites

- Go 1.27 (1.27.3 or later for the standard library's security fixes; `go.mod`'s `toolchain go1.27.3` line has the go command download it when an older 1.27 is installed, unless `GOTOOLCHAIN=local`)
- `nats-server` (v2.15) and the `nats` CLI, for the local dev cluster
- Node.js 22.22, 24.15 or 26 and npm, to build and test the web UI (`make build` verifies or regenerates production assets)
- A C and C++ compiler, because DuckDB is linked through cgo
- `buf`, only to regenerate the API code (`make proto`)

## Setup

1. **Clone the repository**

2. **Build**

   ```sh
   make build
   ```

3. **Start a local cluster, create assets and collect**

   ```sh
   make dev-cluster
   make load
   make run-dev
   ```

   `make run-dev` collects every 10 seconds into `.dev/natslens/natslens.duckdb`, with events from the system account and the APP account, until stopped with Ctrl-C. Its configuration is [scripts/natslens.dev.yaml](../scripts/natslens.dev.yaml). In a second terminal, `make churn` deletes a consumer from a connection named `load-churn`, waits 20 seconds (two collect intervals; `CHURN_WAIT` changes it), recreates it and restarts nats-2.

4. **Query the database** once the collector has stopped (DuckDB lets one process hold the file)

   ```sh
   bin/natslens sql -data-dir .dev/natslens "SELECT epoch, observed_servers, missing_names, partial FROM epochs ORDER BY epoch"
   bin/natslens sql -data-dir .dev/natslens "SELECT time, kind, server_name, stream, consumer, client_name, summary FROM events ORDER BY id"
   ```

5. **Call the API**, from `make run-dev` or, without a cluster, from `make demo`

   ```sh
   curl -s -H 'Content-Type: application/json' -d '{}' http://127.0.0.1:8080/api/natslens.v1.LensService/GetOverview
   ```

6. **Work on the web UI** with hot reload: run `make demo` (or `make run-dev`) in one terminal and `make web-dev` in another, then open http://127.0.0.1:5173. The Vite dev server forwards `/api` to 127.0.0.1:8080; set `NATSLENS_API` to use another address. `make web-test` runs the Vitest tests. `go build` alone embeds existing assets without checking their freshness, or a placeholder when assets are absent. `make build` verifies/rebuilds production assets and embeds them; `make build-full` forces frontend generation. See [build reuse and artifact guarantees](operations/build-reuse.md). Use the [production web-loading runbook](operations/measure-web-loading.md) to measure bundle sizes and browser cost.

## Investigate through the API

`natslens investigate methods` discovers the remote read interface. Copy investigation context from a page or shared chart, then use a trusted profile to query the existing API without browser automation or database access. The [command reference](reference/agent-investigations.md) covers profile/auth setup and JSON; the [agent playbook](agents/investigate.md) covers bounded evidence gathering and citation. Remote and offline sources share the read-only interface. [Storage comparisons](reference/storage-change.md) return their own retained endpoint pins; MCP and universal cross-query pinning remain separate work.

## Demo site

The demo site, https://shadone.github.io/nats-lens/, is the web UI with the API of `natslens demo` compiled to WebAssembly ([cmd/natslens-wasm](../cmd/natslens-wasm)), as static files ([demo mode](features/demo-mode.md)). The [Pages workflow](../.github/workflows/pages.yml) publishes it on a push to `develop`; GitHub Pages must be set to deploy from GitHub Actions.

1. **Build it.** `make demo-site` writes `bin/demo-site`: the UI built with `VITE_NATSLENS_WASM=1` under the base path `DEMO_BASE` (default `/nats-lens/`), `natslens.wasm`, the `wasm_exec.js` of the same Go toolchain, `404.html` (a copy of `index.html`, so a deep link opens the app) and `.nojekyll`. Set `DEMO_SITE` for another directory.
2. **Serve it under its base path.** Any static server works when the directory is reachable as `/nats-lens/`, for example:

   ```sh
   mkdir -p /tmp/pages && ln -sfn "$PWD/bin/demo-site" /tmp/pages/nats-lens
   python3 -m http.server -d /tmp/pages -b 127.0.0.1 8090
   ```

   Open http://127.0.0.1:8090/nats-lens/. Python's server answers a deep link with 404 rather than `404.html`; start from the root.

`natslens.wasm` is about 23 MB, 5.2 MB gzip-compressed, as GitHub Pages serves it. `internal/checks` builds for WebAssembly only without its engine, which reads the store through cgo: `engine.go`, `server_slow.go` and `server_leaf.go` carry a `!(js && wasm)` build tag, and CI, which builds the whole site with `make demo-site` and runs the smoke test on every push and pull request, fails when another import does not build there. The UI's links keep the base path through the router or `web/src/lib/basePath.ts`; `basePath.usage.test.tsx` lists the places that build URLs otherwise.

## Useful commands

| Command               | Description                                                                         |
| --------------------- | ----------------------------------------------------------------------------------- |
| make build            | Verifies/rebuilds production assets, then embeds them in `bin/natslens`.                       |
| make build-go         | Embeds existing assets without verifying freshness; absent assets mean a placeholder.            |
| make web              | Builds the web UI into `internal/webui/dist` (`npm ci` first when the lockfile changed). |
| make web-dev          | Runs the Vite dev server on 127.0.0.1:5173 against the API on 127.0.0.1:8080.       |
| make web-test         | Runs the web UI tests (Vitest and Testing Library).                                 |
| make test             | Runs all Go tests, including local NATS clusters and scale cases.                      |
| make verify-fast      | Runs shared static checks, curated pure Go tests and complete web tests; [scope and prerequisites](operations/verification.md). |
| make verify-full      | Runs static checks, full Go/web tests and selected non-UTC races sequentially. |
| make verify-release   | Runs `verify-full`, then the [upgrade and recovery compatibility matrix](operations/upgrade-compatibility.md); run before tagging. |
| make verify-compat    | Runs the compatibility matrix alone.                                                |
| make test-fast        | Runs the curated pure Go policy/decoding tier, uncached.                           |
| make test-short       | Runs mixed Go integration tests without local NATS or scale cases.                                            |
| make lint             | Runs `go vet` and checks `gofmt`.                                                   |
| make dev-cluster      | Starts nats-0, nats-1 and nats-2 on localhost (`scripts/dev-cluster.sh start`).     |
| make dev-cluster-stop | Stops the dev cluster. `scripts/dev-cluster.sh stop 2` stops only nats-2.           |
| make load             | Creates the example shop's streams, consumers and KV buckets and publishes traffic. |
| make churn            | Deletes a consumer from a named connection, recreates it 20s later, then restarts nats-2. |
| make run-dev          | Runs the collector against the dev cluster.                                         |
| make demo             | Serves the API with the three demo systems on 127.0.0.1:8080, without NATS.         |
| make demo-site        | Builds the [demo site](#demo-site) into `bin/demo-site`.                            |
| make demo-wasm-smoke  | Builds the demo site's WebAssembly and runs one API call through it under Node.     |
| make proto            | Lints the protobuf and regenerates the Go and TypeScript code.                      |
| make fmt              | Formats the Go code with `gofmt -w`.                                                |
| make image            | Builds the linux/amd64 container image with buildx; `IMAGE` names it (default `natslens:dev`). |
| make vuln             | Runs `govulncheck ./...`; fails with an install hint when govulncheck is missing.  |
| make clean            | Removes `bin/`.                                                                     |

The [Go test tier runbook](operations/go-test-tiers.md) lists dependencies, selectors, race checks, and full-suite limits.

The dev cluster uses client ports 4222-4224, cluster ports 6222-6224 and monitoring ports 8222-8224. Its system account user is `sys` and its JetStream account user is `app`; the passwords are local development values in [scripts/dev-cluster.sh](../scripts/dev-cluster.sh). Configuration, logs and data live in `.dev/`, which git ignores.


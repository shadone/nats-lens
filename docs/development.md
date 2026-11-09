# Development

This page is for contributors: what to install, how to build and run nats-lens against a local NATS cluster, and the make targets. How to propose a change is in [CONTRIBUTING.md](../CONTRIBUTING.md).

## Prerequisites

- Go 1.27 (1.27.2 or later for the standard library's security fixes)
- `nats-server` (v2.15) and the `nats` CLI, for the local dev cluster
- Node.js 22.22, 24.15 or 26 and npm, to build and test the web UI (`make build` verifies or regenerates production assets)
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
| make test-fast        | Runs the curated pure Go policy/decoding tier, uncached.                           |
| make test-short       | Runs mixed Go integration tests without local NATS or scale cases.                                            |
| make lint             | Runs `go vet` and checks `gofmt`.                                                   |
| make dev-cluster      | Starts nats-0, nats-1 and nats-2 on localhost (`scripts/dev-cluster.sh start`).     |
| make dev-cluster-stop | Stops the dev cluster. `scripts/dev-cluster.sh stop 2` stops only nats-2.           |
| make load             | Creates the example shop's streams, consumers and KV buckets and publishes traffic. |
| make churn            | Deletes a consumer from a named connection, recreates it 20s later, then restarts nats-2. |
| make run-dev          | Runs the collector against the dev cluster.                                         |
| make demo             | Serves the API with the three demo systems on 127.0.0.1:8080, without NATS.         |
| make proto            | Lints the protobuf and regenerates the Go and TypeScript code.                      |
| make fmt              | Formats the Go code with `gofmt -w`.                                                |
| make image            | Builds the linux/amd64 container image with buildx; `IMAGE` names it (default `natslens:dev`). |
| make vuln             | Runs `govulncheck ./...`; fails with an install hint when govulncheck is missing.  |
| make clean            | Removes `bin/`.                                                                     |

The [Go test tier runbook](operations/go-test-tiers.md) lists dependencies, selectors, race checks, and full-suite limits.

The dev cluster uses client ports 4222-4224, cluster ports 6222-6224 and monitoring ports 8222-8224. Its system account user is `sys` and its JetStream account user is `app`; the passwords are local development values in [scripts/dev-cluster.sh](../scripts/dev-cluster.sh). Configuration, logs and data live in `.dev/`, which git ignores.


# nats-lens

nats-lens observes a NATS deployment: a Go collector takes snapshots and captures events into an embedded DuckDB, SQL checks turn them into findings, and a Connect API serves them to a React UI embedded in the binary. It is read-only towards NATS. The README describes the behaviour; [docs/design.md](docs/design.md) the decisions.

## Layout

| Path | What it holds |
| --- | --- |
| `cmd/natslens` | The `natslens` command: `run`, `once`, `sql`, `checks`, `demo` |
| `internal/collect`, `internal/events`, `internal/k8s` | Snapshots, NATS event capture, the Kubernetes watcher |
| `internal/store` | DuckDB schema (`migrations/`), writes, retention, the Parquet archive |
| `internal/checks`, `checks/*.sql` | The check engine and one SQL file per check |
| `internal/api` | The Connect service; `storeprov` reads the store, `fake` serves `natslens demo` |
| `proto/natslens/v1` | The API; generated code in `internal/gen` and `web/src/gen` (`make proto`) |
| `web/` | React 19, TypeScript and Vite UI, built into `internal/webui/dist` |
| `k8s/` | Kustomize base, a GKE component and example overlays |
| `scripts/` | Local 3-node dev cluster and load generator |

## Commands

- `make build`: the web UI, then `bin/natslens` with it embedded.
- `go test ./...`; `go test -short ./...` skips tests that explicitly check short mode, but still includes database and local-server tests; `go vet ./...`.
- `cd web && npx tsc --noEmit && npm test`.
- `natslens demo`: the API and UI with a fixed scenario on 127.0.0.1:8080, without NATS.
- `make dev-cluster`, `make load`, `make run-dev`: a local cluster, assets and traffic, and the collector against it.

## Rules

- Never mention a company, its services, clusters, hosts or incidents, or any private deployment in this repository. Examples use the online shop of `inventory.example.yaml` and `example.com`. Deployment configuration lives outside the repository; site-specific advice goes into a `checks.remediation_file`.
- Migrations in `internal/store/migrations/` are append-only: never edit one that has been applied anywhere; the `migrations` table holds each file's checksum and natslens refuses to start on a mismatch. A new NOT NULL column on an archived table needs a default.
- Proto changes take new field numbers; never reuse or renumber one. Run `make proto` and commit the generated code.
- Every check has a test in `internal/checks` with one entity that fails and one that does not.
- Every user-facing change updates `docs/features/`: the capability doc and both sections of `docs/features/README.md` (the capabilities table and the coverage map). Then run the feature-docs skill's audit, `python3 <skill>/scripts/audit_feature_docs.py docs/features/ --map-refs require`; its `bad-status` warnings on `Status: planned` docs are expected, every other finding is fixed.

## Agent work

- Treat a named example as evidence of the broader issue; check analogous surfaces before narrowing task scope.
- State the expected benefit of a refactor and verify it afterward: correctness, fewer queries, faster tests/builds, or clearer dependency ownership.
- Follow [Investigation workflows](docs/plans/investigation-workflows.md) for the proposed investigation and agent-access direction; backlog tasks do not establish shipped behavior.
- One implementer and one reviewer per task; the reviewer is a fresh agent and only reads.
- For a fix, see its test fail with the fix taken out, then pass with it, and say so in the report.
- A decision an agent makes is written as "proposed, not yet confirmed", never as the owner's.
- Small conventional commits (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`); a new fix goes in a new commit, never an amend.

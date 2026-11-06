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
| `web/` | React 19, TypeScript and Vite UI, built into `internal/webui/dist`; requests and query hooks in `web/src/api/`, view rules in `web/src/lib/` |
| `k8s/` | Kustomize base, a GKE component and example overlays |
| `scripts/` | Local 3-node dev cluster and load generator |

## Commands

- `make build`: the web UI, then `bin/natslens` with it embedded.
- `go test ./...` (`go test -short ./...` skips the in-process cluster tests); `go vet ./...`.
- `cd web && npx tsc --noEmit && npm test`.
- `natslens demo`: the API and UI with a fixed scenario on 127.0.0.1:8080, without NATS. With `-fixed` it serves 2026-10-17 14:30 UTC, and the UI's live ranges end there.
- `make dev-cluster`, `make load`, `make run-dev`: a local cluster, assets and traffic, and the collector against it.

## Rules

- Never mention a company, its services, clusters, hosts or incidents, or any private deployment in this repository. Examples use the online shop of `inventory.example.yaml` and `example.com`. The design canvas boards' sample names and footnotes come from a real deployment: build pages with the demo's names, never the board's. Deployment configuration lives outside the repository; site-specific advice goes into a `checks.remediation_file`.
- Before database recovery, follow [the recovery procedure](docs/operations/database-recovery.md); test repairs on disposable copies and preserve a verified rollback generation.
- Migrations in `internal/store/migrations/` are append-only: never edit one that has been applied anywhere; the `migrations` table holds each file's checksum and natslens refuses to start on a mismatch. A new NOT NULL column on an archived table needs a default.
- Proto changes take new field numbers; never reuse or renumber one. Run `make proto` and commit the generated code.
- A `time.Time` passed to DuckDB is shifted by the session time zone, which follows the host's, wherever DuckDB cannot type it from a TIMESTAMP column: in arithmetic (`? - INTERVAL 1 HOUR`), as a function argument (`date_trunc('hour', ?)`), or in a subquery's or CTE's select list. Compare a column with the parameter directly (`col >= ?`), or pass Unix microseconds and build the time with `make_timestamp(?::BIGINT)`; test such a query with a non-UTC `TZ`.
- Every check has a test in `internal/checks` with one entity that fails and one that does not.
- Every user-facing change updates `docs/features/`: the capability doc and both sections of `docs/features/README.md` (the capabilities table and the coverage map). Then run the feature-docs skill's audit, `python3 <skill>/scripts/audit_feature_docs.py docs/features/ --map-refs require`; its `bad-status` warnings on `Status: planned` docs are expected, every other finding is fixed.

## Agent work

- One implementer and one reviewer per task; the reviewer is a fresh agent and only reads.
- For a fix, see its test fail with the fix taken out, then pass with it, and say so in the report.
- A decision an agent makes is written as "proposed, not yet confirmed", never as the owner's.
- Small conventional commits (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`); a new fix goes in a new commit, never an amend.

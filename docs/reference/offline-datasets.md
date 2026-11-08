# Offline dataset reference

A dataset is a trusted, private directory containing `manifest.json` and `data.duckdb`. The database is a closed logical copy, not a copy of an open source file. The format and mechanism are proposed, not yet confirmed as owner policy.

## Acquisition commands

The collector accepts these explicit `run` flags:

| Flag | Default | Contract |
| --- | --- | --- |
| `-acquisition-socket` | Disabled | Same-user Unix administrative socket; requires `-acquisition-dir` |
| `-acquisition-dir` | Disabled | Existing private directory owned by the collector user; generated output names only |
| `-acquisition-max-bytes` | 1 GiB | Admission/success budget, cooperatively monitored; at most 4 GiB |

`natslens acquire -socket PATH -system ID` requests one full retained system. The response is JSON with `path` and `manifest`. The path belongs to the collector's filesystem. It is not a client-local copy unless the operator explicitly transfers it. A Unix socket does not connect to a remote host.

Both socket and export directories must have mode 0700 and belong to the collector user. The socket has mode 0600. Use a short socket path: Unix address lengths are platform-limited. The server refuses an existing socket path, including a stale socket. It never removes an existing path to start.

The channel does not use the public HTTP listener, IAP assertions, or the read-only investigation allowlist. Filesystem access is administrative authorization. Every process running as that user can act with that authorization; host/container access controls must protect it. An operator must explicitly authorize any separate remote transport.

## Capture mechanism

The collector attaches a new destination inside private staging, pins its source read transaction, reads provenance, and runs `COPY FROM DATABASE` in that transaction. It commits and checkpoints only the destination, detaches it, and validates it read-only before publication. It never checkpoints, repairs, migrates, copies, or replaces source files during acquisition.

[DuckDB 2.0's logical copy](https://duckdb.org/docs/2.0/sql/statements/copy) includes catalog objects and data. [DuckDB's in-process concurrency](https://duckdb.org/docs/2.0/connect/concurrency) permits concurrent source publication. Tests compare exact rows, definitions, defaults, constraints, indexes, comments, migration checksums, and sequence continuation on synthetic descendants.

A snapshot's rows are published in one transaction. Check runs and finding updates are published later in their own transaction. Events and mutable operator state have separate transactions. The artifact captures committed database state at its read boundary. Its capture-start and export-finish timestamps enclose acquisition; neither timestamp promises pipeline completion. Sequences can advance independently of transactional row visibility and must remain above copied IDs.

## Manifest version 1

All signed 64-bit values use decimal JSON strings, including IDs, byte sizes, counts, interval nanoseconds, and optional retention nanoseconds. Unknown optional values are `null`; timestamps are UTC RFC 3339 values.

| Field | Meaning |
| --- | --- |
| `format_version`, `scope` | `1`, `full_retained_system`; no filtered subset |
| `system` | Captured runtime system ID, name, configured collection interval, optional configured retention |
| `application_version`, `schema_version`, `engine_version` | Build identity, exact migration version, source engine version |
| `capture_started_at`, `exported_at` | Start and finish of acquisition work before publication |
| `boundaries.first_epoch`, `latest_epoch` | Lowest and highest retained completed collection IDs; `null` when empty |
| `boundaries.first_collection_at`, `last_collection_started_at` | Earliest retained start and latest collection's start |
| `boundaries.data_reference` | Latest collection's valid `finished_at`; `null` means no reference clock |
| `boundaries.first_event_at`, `last_event_at`, `last_event_received_at`, `latest_event_id` | Separate event-time and receive-time bounds and latest event ID; not inferred from collection time |
| `boundaries.check_runs`, `latest_evaluated_epoch` | Published run count and highest epoch with any run, including error/disabled status; not proof that checks succeeded |
| `boundaries.pending_evaluation_epochs` | Count of retained collections without any published check run; no unbounded per-epoch list |
| `complete` | Successful artifact preparation; not proof of publication, complete monitoring, or pipeline coverage |
| `pending_stages` | Unknown collection/event buffers and evaluation pipeline completion |
| `exported_facts`, `live_only`, `checks_configuration` | Durable database contents, excluded runtime facts, and `not_exported` checks configuration |
| `data.path`, `size`, `sha256` | Fixed `data.duckdb`, exact byte size, SHA-256 of the closed file |

A latest completion timestamp earlier than its collection start provides no reference clock. No wall-clock fallback is manufactured.

The database contains the migration records and their original checksums. The manifest replaces the source's identity marker and selected safe configuration metadata for offline consumers; source configuration files are not included. Durable notification tables and archive watermarks are captured facts, never instructions to deliver notifications or load missing archive files.

## Validation and compatibility

Names starting with `.dataset-partial-` are reserved for unpublished staging. The public reader rejects those directories even when their prepared manifest has `complete: true`; only the writer can validate staging internally. Publication is the successful atomic rename into a non-staging name. After that rename, a fresh reader can validate the completed artifact after collector or reader restart.

The shared reader accepts exactly two private regular files in a private nonsymlink directory. It rejects path traversal, symlinks, unexpected WAL/partial files, unsupported formats, missing provenance, size/hash mismatch, mismatched recorded boundaries, and incompatible schema or engine metadata. It verifies every migration checksum without rewriting legacy values. It scans every stored column before returning a handle.

The database opens with hard read-only mode, external access disabled, and automatic extension installation/loading disabled. Validation has a cooperative 45-second context. Current schema checksums must match the binary exactly. The manifest engine must equal the reader engine's reported version. Application version is informational; schema and engine compatibility are the enforced gates. Native engine file loading remains a trusted-input boundary, not an untrusted database sandbox.

A bare closed database can use the same read-only opener and boundary inspector without inventing manifest metadata. Its canonical streaming fingerprint requires the caller to hold the validated read-only open handle while hashing and querying. This ties captured continuation to stable file contents. A fingerprint or checksum establishes integrity/coherence, not authenticity.

## Budgets and failures

One process-wide acquisition slot is retained until native work unwinds. There is no job queue. Acquisition requests have a five-minute cooperative context. The command waits at most five minutes plus 20 seconds for HTTP activity; timeout/disconnect cancels server work cooperatively. Native rollback/detach/checkpoint work can outlast cancellation. The collector is never killed or its source engine closed to force cancellation.

Admission requires free space for the full requested byte budget plus 256 MiB headroom. A 20-millisecond monitor observes staging size and remaining free space. Successful database plus manifest bytes must fit the requested budget. Native writes can overshoot between checks or before interrupt processing. The application neither installs nor verifies filesystem quotas. Configure an OS/filesystem quota where hard destination disk isolation is required.

Pinned source readers can retain MVCC versions and WAL history on the source filesystem. Destination quotas do not bound that growth or the engine's shared memory/CPU use. The collector's existing engine memory/thread settings remain in force. Acquisition does not take the store's write mutex; resource contention can still slow collection.

Errors distinguish `busy`, `insufficient_space`, `budget_exceeded`, `cancelled`, and a generic `acquisition_failed`. Native diagnostics are not returned by the administrative channel. Invalid request fields, including server destination paths, fail before acquisition. The CLI exits unsuccessfully on failure.

Publication uses same-filesystem atomic no-replace rename on Linux/macOS after data/manifest validation and sync. Existing artifacts are never overwritten. A parent-directory sync failure after rename reports that publication occurred but durability sync failed. A killed process can leave `.dataset-partial-*`; normal error cleanup removes only its own staging directory. Restart does not scan, publish, or delete leftover directories.

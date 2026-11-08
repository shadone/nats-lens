# Offline datasets

- **Surfaces:** `cli`
- **Status:** shipped
- **Related:** [history-and-archive.md](history-and-archive.md), [sql-access.md](sql-access.md), [running-nats-lens.md](running-nats-lens.md), [Acquire an offline dataset](../operations/acquire-offline-dataset.md), [Dataset reference](../reference/offline-datasets.md)

## What it does

An operator can acquire a consistent offline dataset while the collector runs. An explicitly enabled local administrative socket creates a private, complete artifact on the collector's filesystem. Acquisition needs no collector shutdown and performs no automatic transfer or upload.

## Behavior and rules

- **Full system scope.** A dataset contains every retained table in one system's database, including observations, captured event payloads, findings, inventory, human annotations, and durable notification state. System selection is not account or resource filtering. Existing ingest redaction remains in place; acquisition adds no content redaction.
- **One read boundary.** The collector's DuckDB engine copies the catalog and rows using a single source transaction. Collection rows are atomic within their publication transaction. Checks, captured events, and operator changes have separate publication transactions. The dataset records their published state at the read boundary. In-flight collection, event buffers, and evaluation completion remain explicitly unknown.
- **Provenance.** The versioned manifest records system identity, configured interval and optional retention, application/schema/engine versions, capture/export times, retained collection and event bounds, published check-run summaries, and the latest completed collection's reference time. Empty collection history has no reference clock. Configured retention does not establish continuous history.
- **Durable facts.** The database includes mutes, acknowledgements, inventory, archive watermarks, and durable notification records. It excludes archive files, credentials, runtime configuration, callbacks, and in-memory state. The checks configuration is explicitly unavailable.
- **Private publication.** Acquisition writes a private staging directory, validates the closed database, and publishes a new directory atomically. It refuses an existing destination. An interrupted process can leave a private directory named as partial; the public reader rejects that reserved staging name even if preparation finished.
- **Resource limits.** One acquisition runs per collector process. Admission checks destination free space; monitors cancel work that crosses the requested byte budget or free-space headroom. Native execution and checkpoint cancellation are cooperative. A filesystem quota is required where hard destination isolation is required. A pinned source transaction can also retain source MVCC/WAL history outside that quota.
- **Local administration.** The socket is opt-in, mode 0600, in a private directory owned by the collector user. Requests select a system, never a server path. The command prints a server-local path and manifest. The ordinary API and its read-only investigation allowlist do not expose acquisition.
- **Trusted offline opening.** The shared reader checks private regular files, supported versions, migration checksums, recorded boundaries, size, and SHA-256 before returning a read-only database. External file/network access and extension loading are disabled. Hashes detect damage; they do not authenticate data or make arbitrary database files safe.

## Scenarios

### Acquire while publication continues

- **Given** an operator enabled the private administrative channel for a synthetic system
- **When** the operator requests a dataset while collection, event, check, and operator transactions commit
- **Then** the artifact contains the rows visible at one source transaction boundary
- **And** later publications remain outside that boundary without stopping collection.

### Report pending evaluation honestly

- **Given** a collection is stored before its check results are published
- **When** acquisition captures that boundary
- **Then** the manifest counts collections without a published check run
- **And** it reports evaluation pipeline completion and in-memory buffers as unknown.

### Refuse damage and incomplete output

- **Given** a dataset has a changed data file, incompatible manifest, unexpected file, or mismatched provenance
- **When** an offline consumer opens the dataset
- **Then** validation fails before the consumer receives a database handle.

### Retain a good artifact after failure

- **Given** an artifact already exists and another acquisition is cancelled or exceeds its space or byte budget
- **When** acquisition fails
- **Then** no replacement artifact is published
- **And** cleanup removes only the staging directory created by that acquisition.

## Not supported / out of scope

- Account/resource filtering, message-content export, automatic transfer, automatic upload, or automatic artifact deletion.
- Remote access through the Unix socket, deployment proxy bypass, or default agent access to acquisition.
- Hard application-enforced I/O quotas, hard native cancellation deadlines, or bounding retained source MVCC/WAL with a destination quota.
- Capturing in-memory buffers, proving complete monitoring coverage, evaluating checks during acquisition, repairing the source, or using an hourly archive as an exact-time snapshot.
- Authenticating arbitrary datasets, executing untrusted SQL safely, or opening an active collector's file from another process.

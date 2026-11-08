# SQL access

- **Surfaces:** `cli`
- **Status:** shipped
- **Related:** [history-and-archive.md](history-and-archive.md), [checks.md](checks.md), [command-bar.md](command-bar.md), [design.md](../design.md)

## What it does

`natslens sql` answers trusted offline questions that the pages and proposed semantic investigation commands do not cover. It reads an existing DuckDB database and prints an aligned table or a machine-readable JSON document. Database-only queries need no collector, NATS connection or network.

## Behavior and rules

- **Choose one source.** An explicit database file, a data directory or a configured system selects the source. A multi-system configuration requires a system selection. Missing files fail without creating a database or running migrations.
- **Preserve arbitrary columns.** JSON preserves column order, duplicate labels, type metadata, NULL and exact integer/decimal values. Its arbitrary schema stays separate from the proposed semantic investigation result schema. The [SQL CLI reference](../reference/sql-cli.md) defines representations and flags.
- **Reject unfaithful temporal JSON.** Temporal infinities and dates outside the JSON timestamp format fail with explicit-cast guidance, including nested values. Finite supported values retain their timestamp precision.
- **Keep configuration diagnostics private.** Configuration and system-selection failures omit raw values, paths and configured system IDs. Their fixed diagnostic directs the caller to the local configuration or an explicit offline file.
- **Keep the table.** The default table retains aligned columns, `NULL`, UTC times to milliseconds and the row count.
- **Bound returned data.** Explicit row, output-byte and time budgets bound result presentation. Row truncation is visible. SQL is passed unchanged; no hidden limit changes its meaning. Native execution can consume resources before those limits apply.
- **Publish complete results.** Query, conversion, cancellation and byte-limit failures publish no data. A new output file is published atomically and never replaces an existing file. Diagnostics go to stderr and failures exit unsuccessfully.
- **Protect the selected database.** SQL opens the selected file read-only without migrations or cleanup. Trusted SQL can still read external files, write exports, attach writable databases and use extensions. Read-only database access is not an untrusted SQL sandbox.
- **Select a consistent snapshot.** A live writer lock prevents inspection from another process. Snapshot acquisition is separate work described in [Investigation workflows](../plans/investigation-workflows.md); copying a live database file alone does not establish consistency. The SQL command neither stops the collector nor certifies a supplied snapshot.
- **Keep semantic access separate.** Raw SQL remains an advanced local escape hatch. It is absent from the API and default agent/MCP surface.

## Scenarios

### List the partial collections

- **Given** an offline database with its data in `.dev/natslens`
- **When** I run `natslens sql -data-dir .dev/natslens "SELECT epoch, observed_servers, missing_names, partial FROM epochs ORDER BY epoch"`
- **Then** nats-lens prints one row per collection with the servers that answered, the missing ones, and whether it was partial

### Read the firing findings

- **Given** a copy of a nats-lens database directory
- **When** I run `natslens sql -data-dir DIR "SELECT id, severity, state, since, detail FROM findings WHERE state = 'firing'"`
- **Then** nats-lens prints the firing findings with their severity, start and detail

### Query one of several systems

- **Given** a configuration that lists `shop-prod` and `shop-staging`, and offline database files
- **When** I run `natslens sql -config natslens.yaml -system shop-staging "SELECT count(*) FROM epochs"`
- **Then** nats-lens prints the count from the staging database only
- **And** without `-system` the command fails with a fixed diagnostic that directs me to check the configuration and system selection

### The collector still holds the file

- **Given** `natslens run` is running on the same data directory
- **When** I run `natslens sql` on it
- **Then** the command fails, as DuckDB lets one process open the file

### Preserve machine-readable values

- **Surfaces:** `cli`
- **Given** an offline database and trusted SQL with duplicate column labels, a large integer and a NULL
- **When** I request JSON output
- **Then** the result retains both columns by position, the exact integer as a string and the NULL as null

### Bound and publish a result

- **Surfaces:** `cli`
- **Given** a trusted query returning more rows than the selected budget
- **When** I publish JSON to a new output file
- **Then** the complete document identifies row truncation
- **And** a query error, cancellation or byte-budget failure creates no destination file

### Reject an unfaithful temporal value

- **Surfaces:** `cli`
- **Given** trusted SQL returning a temporal infinity or an out-of-range date inside a nested value
- **When** I request JSON output
- **Then** the command fails without publishing data and directs me to cast the temporal member explicitly to text

### Omit configuration values from diagnostics

- **Surfaces:** `cli`
- **Given** a malformed local configuration containing a sensitive value
- **When** I select that configuration for SQL inspection
- **Then** stderr contains a fixed diagnostic and the process exits unsuccessfully
- **And** stdout and stderr omit the sensitive value and configuration filename

## Not supported / out of scope

- Querying a database held by a live collector process.
- Obtaining or validating a consistent snapshot, bundle source selection or an interchangeable offline semantic backend.
- Executing untrusted SQL, enforcing an OS sandbox, or rolling back external SQL side effects.
- SQL JSON-typed values without an explicit cast to text or binary, because the pinned driver loses JSON precision and duplicate object keys.
- Automatic redaction of arbitrary SQL results, CSV, NDJSON or streaming output.
- A dedicated archive selection mode. Trusted SQL can use DuckDB's external file readers explicitly.

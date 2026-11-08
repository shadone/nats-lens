# SQL access

- **Surfaces:** `cli`
- **Status:** shipped
- **Related:** [history-and-archive.md](history-and-archive.md), [offline-datasets.md](offline-datasets.md), [checks.md](checks.md), [command-bar.md](command-bar.md), [design.md](../design.md)

## What it does

`natslens sql` answers trusted offline questions that the pages and semantic investigation commands do not cover. It reads an existing DuckDB database and prints an aligned table or a machine-readable JSON document. Database-only queries need no collector, NATS connection or network.

## Behavior and rules

- **Choose one source.** An explicit database file, a validated local bundle, a data directory or a configured system selects the source. Bundle and database flags are exclusive with each other and with configuration/system/directory flags. A multi-system configuration requires a system selection. Missing files fail without creating a database or running migrations.
- **Preserve arbitrary columns.** JSON preserves column order, duplicate labels, type metadata, NULL and exact integer/decimal values. Its arbitrary schema stays separate from the semantic investigation result schema. The [SQL CLI reference](../reference/sql-cli.md) defines representations and flags.
- **Reject unfaithful temporal JSON.** TIME, TIMETZ and TIME_NS result types fail before value conversion, including nested members, NULL and empty results. Temporal infinities and dates outside the JSON timestamp format also fail with explicit-cast guidance. Supported date and timestamp types retain their precision.
- **Keep configuration diagnostics private.** Configuration and system-selection failures omit raw values, paths and configured system IDs. Their fixed diagnostic directs the caller to the local configuration or an explicit offline file.
- **Keep the table.** The default table retains aligned columns, `NULL`, UTC times to milliseconds and the row count.
- **Bound returned data.** Explicit row, output-byte and time budgets bound result presentation. Row truncation is visible. SQL is passed unchanged; no hidden limit changes its meaning. Native execution can consume resources before those limits apply.
- **Publish complete results.** Query, conversion, cancellation and byte-limit failures publish no data. A new output file is published atomically and never replaces an existing file. Diagnostics go to stderr and failures exit unsuccessfully.
- **Protect the selected database.** SQL opens the selected file read-only without migrations or cleanup. Trusted SQL can still read external files, write exports, attach writable databases and use extensions. Read-only database access is not an untrusted SQL sandbox.
- **Select a consistent snapshot.** A live writer lock prevents inspection from another process. The [offline dataset acquisition channel](offline-datasets.md) creates a consistent closed artifact while the collector runs; copying a live database file alone does not establish consistency. The SQL command neither stops the collector nor certifies an arbitrary bare file.
- **Validate bundles explicitly.** `-bundle` uses the shared reader before executing trusted SQL. JSON names the bundle and includes its verified manifest within the output budget. Bare `-db` remains schema-independent and does not claim semantic compatibility. Validation does not make arbitrary SQL safe or apply semantic response redaction.

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

### Keep clock distinctions explicit

- **Surfaces:** `cli`
- **Given** trusted SQL returning end-of-day or midnight clock values, either directly or inside a container
- **When** I request JSON output
- **Then** the command rejects the declared clock type without publishing data
- **And** explicitly casting the clock member to text preserves its original SQL representation

### Omit configuration values from diagnostics

- **Surfaces:** `cli`
- **Given** a malformed local configuration containing a sensitive value
- **When** I select that configuration for SQL inspection
- **Then** stderr contains a fixed diagnostic and the process exits unsuccessfully
- **And** stdout and stderr omit the sensitive value and configuration filename

### Inspect a validated bundle

- **Surfaces:** `cli`
- **Given** a trusted published local dataset
- **When** I select `-bundle` with trusted SQL and JSON output
- **Then** shared validation checks compatibility and integrity before execution
- **And** the bounded result includes the manifest while retaining exact positional SQL values.

## Not supported / out of scope

- SQL over the API. [Agent investigations](agent-investigations.md) use bounded semantic reads against live APIs or closed datasets.
- Querying a database held by a live collector process.
- Acquiring a snapshot from SQL itself; use the [dataset acquisition workflow](offline-datasets.md) separately.
- Executing untrusted SQL, enforcing an OS sandbox, or rolling back external SQL side effects.
- SQL JSON-typed values without an explicit cast to text or binary, because the pinned driver loses JSON precision and duplicate object keys.
- JSON output of TIME, TIMETZ or TIME_NS typed values without an explicit cast to text, because the pinned driver loses clock distinctions or lacks a decoder.
- Automatic redaction of arbitrary SQL results, CSV, NDJSON or streaming output.
- A dedicated archive selection mode. Trusted SQL can use DuckDB's external file readers explicitly.

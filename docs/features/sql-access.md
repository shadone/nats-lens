# SQL access

- **Surfaces:** `cli`
- **Status:** shipped
- **Related:** [history-and-archive.md](history-and-archive.md), [checks.md](checks.md), [command-bar.md](command-bar.md), [design.md](../design.md)

## What it does

Everything nats-lens knows is in its DuckDB database, and `natslens sql` runs one SQL query against it and prints the result as a table. It answers questions that no page answers yet, and it is how a check author tries a query. The database must not be open in a running collector at the time, because DuckDB lets one process hold the file.

## Behavior and rules

- **One query per call.** `natslens sql [-data-dir DIR] QUERY` opens `natslens.duckdb` in `DIR` (`./data` by default) read-only, runs the query and prints the rows as an aligned table, followed by the row count, for example "(3 rows)".
- **Plain output.** A missing value prints as `NULL`, and a time prints in UTC to the millisecond.
- **Read-only.** The database is opened read-only, so a query cannot change it.
- **Stopped collector, or a copy.** While `natslens run` holds the file, `natslens sql` fails. Stop the collector first, or query a copy of the database directory.
- **The schema explains itself.** Every table and column carries a comment. `SELECT table_name, column_name, comment FROM duckdb_columns() WHERE NOT internal ORDER BY table_name` lists them.
- **One row per server's view.** Statistics tables hold one row per server per collection; identity tables one row per thing with its first and last collection; configuration tables one row per server per run of collections with the same configuration. A server gets a new `server_id` on every restart, so queries join the `servers` table for the stable `server_name`.

## Scenarios

### List the partial collections

- **Given** a stopped nats-lens with its data in `.dev/natslens`
- **When** I run `natslens sql -data-dir .dev/natslens "SELECT epoch, observed_servers, missing_names, partial FROM epochs ORDER BY epoch"`
- **Then** nats-lens prints one row per collection with the servers that answered, the missing ones, and whether it was partial

### Read the firing findings

- **Given** a copy of a nats-lens database directory
- **When** I run `natslens sql -data-dir DIR "SELECT id, severity, state, since, detail FROM findings WHERE state = 'firing'"`
- **Then** nats-lens prints the firing findings with their severity, start and detail

### The collector still holds the file

- **Given** `natslens run` is running on the same data directory
- **When** I run `natslens sql` on it
- **Then** the command fails, as DuckDB lets one process open the file

## Not supported / out of scope

- **No SQL over the API.** The API has no query endpoint; a read-only SQL endpoint and a server for AI agents are being considered for after the first release.
- **No query while collecting.** The database is locked by the running collector.
- **No archive queries.** `natslens sql` reads the database only; hours older than `retention` are in the Parquet archive, which DuckDB can read directly.
- **No output formats.** The output is an aligned text table; there is no CSV or JSON option.

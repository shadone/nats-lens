# History and archive

- **Surfaces:** `ui`, `cli`
- **Status:** shipped
- **Related:** [offline-datasets.md](offline-datasets.md), [monitoring-snapshots.md](monitoring-snapshots.md), [event-capture.md](event-capture.md), [data-history.md](data-history.md), [sql-access.md](sql-access.md), [running-nats-lens.md](running-nats-lens.md), [design.md](../design.md)

## What it does

nats-lens keeps its collections, events and findings in an embedded DuckDB database for `retention` (14 days by default). Every hour it also writes that hour's rows to Parquet files in an archive directory, which keeps them for `archive_retention` (90 days by default). When the database file is lost, nats-lens rebuilds from successful manifested exports at start. Unexported activity is not recoverable from the archive.

## Behavior and rules

- **Archive state in the UI.** [Data history](data-history.md) shows the archive’s latest known hours, bounds, listing time, export state, and export error. The listing refreshes at most once an hour after a successful export; later exports update its known hours. Before the first listing, the hours read “not listed yet”. Selecting a past range does not rewind the archive state.

- **Retention.** Once an hour, nats-lens deletes the collections and events older than `retention`, together with the findings' transitions and check runs of those collections. Mutes are never deleted. A finding that is still firing keeps its current episode, so it still says when it started even after the collection it started in is gone.
- **The database lives in `data_dir`.** The file is `natslens.duckdb` in `data_dir` (`./data` by default). DuckDB lets one process open the file while it is written, so `natslens sql` and `natslens checks run` work only while no collector holds it.
- **Engine compatibility.** nats-lens uses DuckDB 2.0 and reads databases created by the earlier engine. New database files use the default 2.0 storage format, which engines before 2.0 cannot read. Keep a verified pre-upgrade generation for rollback; opening a database with an upgraded binary does not repair existing index divergence. The [database recovery procedure](../operations/database-recovery.md) describes validation before replacement.
- **Schema upgrades and downgrades.** At start, nats-lens applies the schema migrations its database lacks. It refuses a database whose schema is newer than its own, written by a later release, with "database schema version N is newer than this binary (M)", and changes nothing in it. It never converts a database back to an older schema: to return to an older release, restore the backup taken before the upgrade. `natslens checks run` names a newer database the same way.
- **Hourly Parquet files.** For every table, an hour's rows go to `archive_dir/<table>/date=YYYY-MM-DD/hour=HH.<export>.parquet` (`archive_dir` defaults to `data_dir/archive`). A manifest per hour, written last, lists exactly the files of that export with their row counts. A crash during an export therefore leaves the previous manifest and its files intact.
- **The open hour is written early.** nats-lens writes the hour that is still open provisionally every `archive_provisional_every` (5 minutes), and again once the hour has closed. Recovery reaches the latest successful manifested export; disabled, failed or delayed exports can leave a larger gap.
- **Late rows are written again.** An hour whose rows change after it was exported, such as an event that arrived late, is exported again. Hours missed while nats-lens was down are caught up, at most six hours per collect interval, so catching up never delays a collection. A closed hour that still waits for its export an hour after it ended, or after nats-lens started, raises `system-008` ([system-checks.md](system-checks.md)); a failed export is also logged as "archive export failed".
- **Mutes and findings in every hour.** Every hour holds the whole `findings` table and the whole `mutes` table as they were at its export. When a mute is created or lifted, nats-lens exports the newest hour again, so the newest hour always carries the current mutes.
- **Archive retention.** After a successful export, at most once an hour, nats-lens removes the archived hours that ended more than `archive_retention` ago: their data files first, then their manifest. `0` keeps every hour. nats-lens refuses an `archive_retention` shorter than `retention`, because a rebuild must find every hour the database holds.
- **One writer.** The archive directory expects one nats-lens writing to it at a time. Two that overlap for a while, as in a rollout, leave unused files behind but never a manifest that names a missing file.
- **Keep the archive apart.** nats-lens logs a warning at start when an `archive_dir` set in the configuration is inside the data directory: losing that one volume loses the archive too. The default archive directory, `archive` inside the data directory, is not warned about.
- **Rebuild at start.** When the database file is missing, or holds no collection and no event, and the archive has complete hours inside the retention window, nats-lens rebuilds the database from them before it serves anything. It loads only hours with a manifest, and stops when a file's row count differs from its manifest. It writes the new database beside the old name and renames it into place only when the load succeeded, so a failed rebuild is tried again on the next start.
- **A damaged database is kept.** When DuckDB cannot replay the database's write-ahead log, nats-lens renames the file and its log to `<name>.corrupt-<UTC time>`, rebuilds from the archive, logs an error and records a `collector.database_recovered` event that names the kept files. It never deletes them.
- **Past ranges show the past.** When the UI's time range ends in the past, the findings and statuses are rebuilt from the stored transitions as they stood at the end of the range, with the mutes in force then.
- **Where history begins.** An unresolved finding whose condition predates history, or which was first seen within a minute after history starts, says "already true when nats-lens started".

- **Offline acquisition.** The [offline dataset channel](offline-datasets.md) creates a full-system artifact at one database read boundary. Hourly recovery archives retain their own publication boundaries and are not exact-time investigation snapshots.

## Scenarios

### The volume of the database is lost

- **Surfaces:** `cli`
- **Given** the database and the archive are on separate volumes, and the database volume is replaced by an empty one
- **When** nats-lens starts
- **Then** it rebuilds `natslens.duckdb` from the archived hours within `retention`, and only then starts serving
- **And** the collections, events, findings and mutes up to the last export are back

### The database cannot be opened after a crash

- **Surfaces:** `cli`
- **Given** a database whose write-ahead log DuckDB cannot replay
- **When** nats-lens starts
- **Then** it renames both files to `natslens.duckdb.corrupt-<UTC time>` and `natslens.duckdb.wal.corrupt-<UTC time>`, rebuilds from the archive and logs an error
- **And** the Events page shows a `collector.database_recovered` event that names the kept files

### Create a database and reopen it

- **Surfaces:** `cli`
- **Given** nats-lens creates a database with its embedded DuckDB 2.0 engine
- **When** nats-lens checkpoints, closes, and reopens the database
- **Then** the stored rows and unique constraints remain available
- **And** an older DuckDB engine cannot open the new database format

### Start an older release on a newer database

- **Surfaces:** `cli`
- **Given** a database that a later nats-lens release has upgraded
- **When** an older release starts on it
- **Then** start fails with "database schema version N is newer than this binary (M)"
- **And** the database file is unchanged, so the later release still opens it

### Look at last week

- **Surfaces:** `ui`
- **Given** nats-lens has collected for ten days with the default retention
- **When** I choose "Last 7d" or a custom range eight days back
- **Then** every page shows the data of that range, and the findings as they stood at its end

### Keep the archive for a year

- **Surfaces:** `cli`
- **Given** a volume large enough for a year of hourly files
- **When** I set `archive_retention: 365d`
- **Then** nats-lens removes archived hours only once they are older than a year
- **And** `archive_retention: 7d` with `retention: 14d` stops the start with an error, as a rebuild would miss hours

### Inspect the latest archive state

- **Surfaces:** `ui`
- **Given** the archive has listed its hours and completed a provisional export
- **When** I open Data history with a past range
- **Then** its archive card names the latest known hours and export, including that the newest hour was provisional

## Not supported / out of scope

- **The UI reads the database only.** Hours that exist only in the archive, older than `retention`, cannot be shown in the UI or queried with `natslens sql`. Read the Parquet files with DuckDB or another tool.
- **No bucket destination.** The archive is a local directory; an object storage destination is not built.
- **No automatic index repair.** An engine upgrade does not repair pre-existing index divergence or establish that every index is sound.
- **No automatic downgrade.** An older release never converts a newer database; restore the pre-upgrade backup instead.
- **No partial rebuild.** A rebuild runs only for a missing or empty database, or for one whose write-ahead log cannot be replayed. It does not fill gaps in a database that exists.
- **The inventory is not archived.** nats-lens loads the inventory file on every start instead.
- **No high availability.** Two nats-lens processes cannot share one database, and the archive expects one writer.

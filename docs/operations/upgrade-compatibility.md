# Check upgrade and recovery compatibility

Run this check before a release, and after any change to migrations, the archive, rebuild or the DuckDB driver. It shows that a database or archive written by any earlier schema of this repository opens, upgrades and rebuilds under the current binary, and that a newer database is refused without a write. The supported boundaries, the downgrade rule and the release step below are **proposed, not yet confirmed**.

The check uses generated fixtures in disposable directories only. It does not upgrade the DuckDB engine, touch a live database, or authorize a recovery; for a damaged database follow the [recovery procedure](database-recovery.md).

## Run the check

```sh
make verify-compat     # the compatibility matrix alone
make verify-release    # the full profile, then the matrix: the release gate
```

Both run [`scripts/verify.py`](../../scripts/verify.py). The `compat` profile has two stages:

| Stage | Command | Environment |
| --- | --- | --- |
| `compat-store` | `go test -count=1 -timeout 40m ./internal/store -run '^(TestCompat[A-Za-z]+\|...)$'` with the migration, archive, crash and recovery tests listed in `verify.py` | `NATSLENS_COMPAT_MATRIX=1`, `TZ=Pacific/Honolulu` |
| `compat-cmd` | `go test -count=1 -timeout 10m ./cmd/natslens -run '^(TestFailedRebuildIsRetried\|TestEmptyDatabaseIsRebuilt\|TestDatabaseWithDataIsKept\|TestUnreplayableWALIsMovedAsideAndRebuilt)$'` | the same |

The runner first lists each package's tests and fails when a selected name, or a pattern such as `TestCompat[A-Za-z]+`, matches none, so a renamed test cannot drop out of the gate unnoticed. `NATSLENS_COMPAT_MATRIX=1` makes the `TestCompat` tests start from every schema version. Without it, as in `go test ./...`, `make verify-full` and CI, they start from the seed versions 1, 35, 37 and the newest only (`compatSeeds`), and the interrupted-upgrade test from version 1 only. The seeds stay in the normal tier as regression tests; the whole matrix stays out of the edit loop because it rebuilds a fixture per version.

To run one part by hand:

```sh
TZ=Pacific/Honolulu NATSLENS_COMPAT_MATRIX=1 go test -count=1 -timeout 40m ./internal/store -run '^TestCompat'
```

## Versions under test

| Component | Version | Source |
| --- | --- | --- |
| Go | `go 1.27.1`, built with `toolchain go1.27.3` | `go.mod`; the Dockerfile's `golang:1.27.3-bookworm` |
| DuckDB driver | `github.com/duckdb/duckdb-go/v2 v2.20000.0` | `go.mod` |
| Driver bindings | `github.com/duckdb/duckdb-go-bindings v0.20000.0`, `go.duckdb.org/libduckdb/<os>-<arch> v0.20000.0` | `go.mod` |
| DuckDB engine | `v2.0.0`, source `d54cbbe3df` | `SELECT library_version, source_id FROM pragma_version()` |
| Storage format | the engine's default for new files (2.0) | [design.md](../design.md) |
| Schema | migrations `0001` to `0041` | `internal/store/migrations/` |

Record these again when `go.mod` changes; the matrix result applies to these versions only.

## Supported boundaries

Proposed, not yet confirmed:

- **Upgrade.** The current binary opens a database at any schema version from `0001` to the newest and applies the missing migrations at start. Every release keeps that range: migrations are append-only, so an older database always has a prefix of the current set.
- **Archive.** A rebuild loads archived hours written by any schema version, alone or together with hours written by the current one. A column added after an hour was written is NULL or its default in that hour's rows.
- **Interruption.** A crash during an upgrade, an export or a rebuild leaves files that the next start either finishes (upgrade), ignores (an export without a manifest) or retries (a rebuild that never replaced the database).
- **Engine.** DuckDB 2.0 opens files written by DuckDB 1.5.6. It writes new files in the 2.0 format, which 1.5.6 cannot read.
- **Downgrade.** None. An older binary refuses a database that a later release upgraded and leaves it and its write-ahead log unchanged, unless it cannot replay that log (see [Downgrade behaviour](#downgrade-behaviour)). Going back to an older release means restoring the backup taken before the upgrade.

## Matrix

| Combination | Test | Normal tier | Release matrix |
| --- | --- | --- | --- |
| Database at schema v, opened by the current binary | `TestCompatUpgradeFromOlderSchemas` | v = 1, 35, 37, 41 | v = 1 to 41 |
| Upgrade from v interrupted after about half the pending migrations, then restarted | `TestCompatUpgradeInterruptedThenRestarted` | v = 1 | v = 1 to 40 |
| Archive hour written at schema v, rebuilt alone | `TestCompatRebuildFromOlderArchives/from_v/alone` | v = 1, 35, 37, 41 | v = 1 to 41 |
| Archive hour at schema v and one at the newest, rebuilt together | `TestCompatRebuildFromOlderArchives/from_v/mixed` | v = 1, 35, 37, 41 | v = 1 to 41 |
| Database written by a later binary (unknown migration) | `TestNewerDatabaseIsRefusedBeforeAnyWrite` (with and without a write-ahead log), `TestNewerMigrationOnlyInTheLogIsRefused`, `TestCheckSchemaNamesANewerDatabase` | yes | yes |
| Database with an edited migration | `TestEditedMigrationRefusedBeforeBackfill`, `TestEditedMigrationFailsStart`, `TestEditedMigrationStopsPendingOnes` | yes | yes |
| Migration files changed, removed or added without a pinned checksum | `TestMigrationsMatchGolden` | yes | yes |
| Column added later to an archived table is nullable; no archived column becomes NOT NULL later | `TestCompatArchivedColumnsAddedLaterAreNullable` | yes | yes |
| Every table is archived or deliberately not | `TestCompatEveryTableArchivedOrListed` | yes | yes |
| File written by DuckDB 1.5.6, opened by 2.0 | `TestFatalEngineContainment` (the `internal/testfatal` fixture) | yes | yes |

### What each upgrade and rebuild checks

After an upgrade from version v, `compatCheckUpgraded` and `compatStoreReads` check:

- **Schema.** Every column with its type, default, nullability and comment, every constraint, index, view, sequence and table comment equals a new database's. A sequence's start value is left out: after a reopen DuckDB reports the next value there.
- **Migration records.** Every migration is recorded once with its checksum.
- **Rows.** Every row of the fixture is in the upgraded table and the other way round, compared on the fixture's columns with `EXCEPT ALL` in both directions. Each column the upgrade added holds its default, or NULL without one.
- **Backfills and notification outcomes.** From before `0036`, a finished message's `finished_at` is its delivery time, else its queue time, and each channel's delivery watermark is its newest outcome. From before `0038`, `notify_state.acknowledged` is set only where an acknowledgement in force existed.
- **Identities and sequences.** The delivery identity (`notify_identity`) is kept by the upgrade and by a reopen, or created once when the upgrade adds the table. Every sequence the fixture had continues after the fixture's last value; one the upgrade creates starts at its start value. A collection and a mute written after the upgrade get numbers after the fixture's.
- **Findings, mutes, acknowledgements and acceptances.** The store reads them through `Findings`, `Finding`, `ListMutes`, `ListAcceptances` and `Acks`, and the counts match the tables.
- **Fixture untouched.** Every run works on a copy, and the fixture's SHA-256 is the same afterwards.

After a rebuild, `compatCheckRebuilt` checks that every archived row is in its table with the values its file has, that the table holds no other rows, that a column a file lacks is NULL or its default in that file's rows, that the epoch, event, mute and acknowledgement sequences continue after the highest loaded number, and the same store reads.

## How the fixtures are made

No binary fixture is stored for the schema matrix. `buildCompatFixture` (`internal/store/compat_fixture_test.go`) builds a database at version v from the repository alone:

1. Create the `migrations` table as `migrate` does before `0010`.
2. Apply the embedded migrations `0001` to v, one per transaction, as an older binary did. `TestMigrationsMatchGolden` pins each file's checksum, so the same v always gives the same schema.
3. After each migration, write rows 1 and 2 to each table it created and left empty, with the columns the table has then. At v, write row 3 to every table with all its columns. `migrations` and `notify_identity` get no generated rows.
4. Advance every sequence seven times, then checkpoint and close, so the file holds everything without a write-ahead log.

Each value depends only on the row number, the column's name and type, and a fixed hour on 2026-10-17 UTC. Row i's times fall in minute i of that hour, at a second derived from the column's name, so two time columns of a row differ; the columns a backfill compares (`compatLinkedSeconds`: `baselined_at`, `sent_at`, `created_at`, `reconciled_at`) have pinned seconds. A few columns take values their CHECK constraints allow. A type the generator does not know fails the build with its name, so a migration with a new column type extends the generator. The names are generic (`shop-<column>-<n>`).

An archive hour is `compatExport` of a fixture: each archived table's rows for the hour, `SELECT *`, to one Parquet file, as the export of that schema version wrote them. The mixed case adds an hour from a fixture at the newest version with other row numbers and another hour.

The engine fixture `internal/testfatal/testdata/index-divergence-1.5.6.duckdb.gz` (15 KB) is the one binary file. Its [README](../../internal/testfatal/testdata/README.md) says how `generate.go` made it with DuckDB 1.5.6.

## Migration rules the check enforces

- **Append-only and checksummed.** `TestMigrationsMatchGolden` fails when a migration file changes, is removed, or is added without its checksum pinned. At start, a recorded checksum that differs from its file stops the binary (`TestEditedMigrationFailsStart`).
- **Columns added later to archived tables are nullable.** A rebuild reads a table's hours with `union_by_name`. When one file has a column and another does not, the rows of the older file get NULL there; a column default does not apply, so a NOT NULL column fails the load even with a default. `TestCompatArchivedColumnsAddedLaterAreNullable` enforces AGENTS.md's rule that such a column is nullable, even with a default. It also fails when a migration makes an existing nullable column of an archived table NOT NULL, since older files can hold NULL there, and it checks itself against both cases. `TestCompatNotNullDefaultFailsMixedRebuild` pins the engine behaviour the rule rests on.
- **Every table is accounted for.** A table must be in `archiveTables` or in `compatUnarchived`, so a new table is not left out of the archive unnoticed.
- **Manifests.** A rebuild loads only hours with a manifest and stops when a file's row count differs from it (`TestRebuildNeedsManifest`, `TestRebuildChecksRowCounts`). A re-export that stops before its manifest leaves the previous export whole (`TestReexportCrashKeepsPreviousExport`).

## Downgrade behaviour

Proposed, not yet confirmed: nats-lens never converts a database back to an older schema.

- An older binary that opens a database with a migration newer than its own fails at start with `database schema version N is newer than this binary (M): it was written by a later natslens; run that release or a later one, or restore a backup taken before the upgrade`. The check reads the `migrations` table on a read-only open, before the binary opens the file for writing, so the database file and any write-ahead log the later release left are byte for byte unchanged (`TestNewerDatabaseIsRefusedBeforeAnyWrite`, with and without a log). A check on the read-write handle would not be enough: closing that handle checkpoints, which merges the log into the file and removes it. Binaries built before this check also refuse, but only after rewriting the migrations table's comments, and closing their handle then checkpoints any write-ahead log the later release left into the file.
- An edited migration is refused the same way, before missing checksums are filled in (`TestEditedMigrationRefusedBeforeBackfill`).
- A write-ahead log that the older engine cannot replay is not refused this way. The read-only open fails with a replay error, and the collector takes its recovery path for an unreplayable log: it renames the file and the log to `<name>.corrupt-<UTC time>` and rebuilds from the archive. The renamed files are kept unchanged, but the older release then runs on a rebuilt database; restore the pre-upgrade backup instead of continuing on it.
- `natslens checks run` and the offline readers open the file read-only and refuse a newer schema without writing.
- A newer engine format is not detected by nats-lens: DuckDB 1.5.6 cannot open a 2.0 file and reports its own error. nats-lens adds no engine-format check, because the driver offers no safe way to read the format without opening the file.

## What is not tested

- A downgrade of the engine: no test runs DuckDB 1.5.6 against a 2.0 file in this repository. The design records a disposable probe in which 1.5.6 rejected a default 2.0 file.
- Databases written by a binary built from another branch whose migrations differ from `develop`'s at the same number. `verifyChecksums` refuses them; there is no fixture.
- Real collector data and sizes: the fixtures hold three generic rows per table.
- Archives on another filesystem, a bucket, or a different operating system.
- Disk full, power loss and file-system reordering beyond a copy of the file and its write-ahead log.
- Index divergence and its repair: the [recovery procedure](database-recovery.md) and `TestFatalEngineContainment` cover detection, not repair.

## Backup and rollback prerequisites

Before upgrading a deployed nats-lens across a release with a new migration:

1. Stop the collector and every process that opens the database.
2. Copy the complete data directory: `natslens.duckdb`, its write-ahead log if present, and `system.json`. Record the files' SHA-256 and keep the copy outside the data directory, as the [recovery procedure](database-recovery.md#preserve-the-source-generation) describes.
3. Keep the archive directory: a rebuild from it is the other way back.
4. Start the new release and check startup, a collection and an export.

The first start of a release with migration 0041 or later, on a database written before it, fills the asset counts of every stored epoch (`epoch_asset_counts`) before the API listens: about 1 ms per epoch at 2,000 streams and 10,000 consumers, so about a minute for 14 days. The log says `asset counts filled` with the number of epochs and the time. A fill that fails is logged as a warning, and the Overview's counts are then read from the reports, slower but the same, until a later start fills them. Proposed, not yet confirmed.

To roll back, stop the new release and restore the copied directory whole; never combine a restored file with a newer write-ahead log. Writes the new release made since the upgrade are lost, except what its exports wrote to the archive. Starting the old release on the upgraded database is refused and changes neither the file nor its write-ahead log, so a mistaken start needs no repair. The exception is a log the old engine cannot replay: the old release moves the file and log aside and rebuilds from the archive (see [Downgrade behaviour](#downgrade-behaviour)).

## Results

| Date | Revision | Command | Result |
| --- | --- | --- | --- |
| 2026-11-11 | `69867e4a` (before the fix round; documentation uncommitted) | `python3 -B scripts/verify.py compat` | Pass: `compat-store` 589 s, `compat-cmd` 25 s, on a laptop under a load average of about 35 from other work |
| 2026-11-11 | `f57203fe` | `TZ=Pacific/Honolulu NATSLENS_COMPAT_MATRIX=1 go test -count=1 -run '^TestCompat' -v ./internal/store` | Pass in 506 s: upgrade from all 40 versions, interrupted upgrade from 39, archive rebuild alone and mixed from 40 (80 cases), the three rules tests |
| 2026-11-12 | branch `perf/asset-counts` (migration 0041), rebased onto develop `c5cb0879` | `TZ=Pacific/Honolulu NATSLENS_COMPAT_MATRIX=1 go test -count=1 -timeout 40m ./internal/store -run '^TestCompat'` | Pass in 240 s at a load average of 5 to 6: upgrade from all 41 versions, interrupted upgrade from 40, archive rebuild alone and mixed from 41 (82 cases), the three rules tests |

Times depend on host load; the matrix grows by about 12 s per new migration.

## Tests this check reuses

The matrix builds on coverage from earlier work and runs it in the same stage:

| Area | Tests |
| --- | --- |
| Migrations and checksums | `TestMigrationsMatchGolden`, `TestAcceptedEarlierChecksumOpens`, `TestMigrationChecksumsRecorded`, `TestEditedMigrationFailsStart`, `TestMissingChecksumsBackfilled`, `TestEditedMigrationStopsPendingOnes`, `TestChecksumRecordedWithMigration` |
| Upgrades of populated databases | `TestUpgradeFromVersion9`, `TestMigrationsFrom21OnAPopulatedDatabase`, `TestMigrationsFrom26OnAPopulatedDatabase`, `TestMigrationsFrom30OnMutes`, `TestMigrationsFrom31OnAPopulatedDatabase`, `TestMigrationsFrom32OnAPopulatedDatabase`, `TestSourceHealthUpgrade`, `TestNotificationIdentityUpgrade`, `TestNotifyAcknowledgementMemoryUpgrade`, `TestNotifyAcknowledgementUpgradeEvidence`, `TestNotifyAcknowledgementUpgradeBaselineBoundaries` |
| Crash and write-ahead log | `TestReopenAfterCrashAfterOpen`, `TestReopenAfterSchemaWALReplay`, `TestUnreplayableWALIsErrWALReplay`, `TestIndexSurvivesWALReplayShutdown`, `TestIndexReplayProcess`, `TestUnreplayableWALIsMovedAsideAndRebuilt` |
| Typed fatal errors and the 1.5.6 engine file | `TestFatalBoundaryRefusesNativeOperations`, `TestFatalEngineContainment` |
| Export, manifest and rebuild | `TestArchiveRoundTrip`, `TestRebuildNeedsManifest`, `TestRebuildChecksRowCounts`, `TestReexportCrashKeepsPreviousExport`, `TestExportIDNotReused`, `TestConcurrentExportsKeepRebuildWorking`, `TestCleanupKeepsLaterExports`, `TestStateRaisesSequences`, `TestStateRaisesMuteSequence`, `TestFailedRebuildIsRetried`, `TestEmptyDatabaseIsRebuilt`, `TestDatabaseWithDataIsKept` |
| Archives from before a migration | `TestRebuildMixesMutesFilesFromBeforeAndAfterAcceptances`, `TestRebuildFromOldMutesFilesOnlyAddsNoRow`, `TestRebuildBeforeAcceptancesLoadsPlainMutes`, `TestRebuildBeforeAcknowledgements`, `TestRebuildBeforeServerLimitsLoadsNull`, `TestArchiveBeforeSourceHealth` |
| Copies of a live database | `TestDatasetCatalogAndContentsConserved` |

# Recover a database after an index failure

Use this procedure when a stored database opens but a database operation fails with a fatal index error. Preserve the original files and validate a separate recovery candidate before replacing an active database.

The owner must authorize access to private data and activation of a repaired generation. Existing authorization applies to its stated scope; permission to diagnose copies does not authorize replacing an active database.

## Preserve the source generation

1. Stop the collector and every other process that opens the affected database. Verify that no process holds the database or its write-ahead log (WAL) open. If a writer remains, stop before copying.
2. Copy the complete affected data directory, including `natslens.duckdb`, its matching WAL and sidecars, and `system.json`. Store the backup outside the active directory with directory mode 0700 and file mode 0600.
3. Record the file inventory, sizes and SHA-256 hashes. Compare source hashes before and after copying, and compare every backup hash. If files change, acquire a fresh stopped snapshot.
4. Preserve that backup unchanged. Create disposable descendants for every engine open, diagnostic or repair experiment. Keep private rows, identifiers and raw fatal chunks out of repository files and shared reports.

## Diagnose and validate a candidate

1. Record the driver and embedded engine versions. Read schema and summary counts on a disposable copy; check prohibited NULLs and duplicate keys with full scans. Compare index behavior with table contents where the error concerns an index.
2. Reproduce the failing operation on another disposable copy, including transaction commit. A successful DELETE followed by rollback does not test a commit-time failure. After a typed fatal error, stop using that engine handle.
3. Evaluate a repair on a new copy. Recreating one index does not establish that other indexes are sound. An engine upgrade or successful checkpoint does not establish repair of existing divergence.
4. Compare every table before and after recovery with bidirectional `EXCEPT ALL` or an equivalent exact multiset comparison. Check keys and prohibited NULLs separately; equal counts alone do not prove equal rows.
5. Compare table and view definitions, defaults, constraints, indexes, table and column comments, view comments, migration records and checksums, and sequence definitions. Compare each sequence's next values on separate descendants so the candidate's sequences remain untouched.
6. Exercise committed writes and unique, CHECK and NOT NULL constraints on disposable descendants. Close and reopen those copies, run committed retention sweeps, and compare every table with independently calculated expected survivors. Preserve acknowledgement, mute, notification and identity semantics.
7. Checkpoint and close the unswept candidate, then record its complete file inventory and hashes. Verify copies after reopen. Obtain a fresh read-only review of that exact generation before requesting activation approval.

Archive recovery follows [the archive's successful export coverage](../features/history-and-archive.md). Check manifests, file row counts and export boundaries before choosing it; unexported activity can be absent.

## Activate the reviewed generation

Replacing a database can discard intervening writes. Pairing a rebuilt file with an older WAL can replay an incompatible generation. Confirm the following steps before activation.

1. Obtain authorization for the exact reviewed candidate and activation scope. Record the rollback paths and stop procedure before changing files.
2. Verify that the collector remains stopped, no database handles are open, and the active file inventory and hashes still match the preserved snapshot. If the source changed, preserve a fresh snapshot and repeat recovery validation instead of discarding new activity.
3. Stage the reviewed candidate beside the active directory on the same filesystem. Carry its matching `system.json`, preserve restrictive permissions and verify every hash. Exclude the old generation's WAL and sidecars.
4. Rename the complete original generation into a rollback directory. Rename the staged directory into place while writers remain stopped. If activation fails, restore the original directory before allowing a start. Sync the changed files and directory entries.
5. Verify the installed hashes and identity file, and verify that the retained original generation still matches its hashes. Keep the preserved backup and reviewed candidate unchanged.
6. Start the collector only within the authorized scope. Check startup identity, bounded API reads, fresh collections, a successful committed retention sweep and configured archive exports. Process readiness alone does not prove collection or event capture.

## Roll back or continue observation

If startup or retention fails, stop the collector before rollback. Preserve the failed generation and its matching WAL for diagnosis. Restore the complete original generation only after reconciling any writes made since activation; never combine generations.

Recovery proves the checks exercised on that candidate. Continue observation for recurrence, and validate any prevention fix separately. The [fatal containment design](../design.md#fatal-database-containment-proposed-not-yet-confirmed) describes the running process's response to a typed fatal database error.

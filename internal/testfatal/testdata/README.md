# Synthetic index-divergence fixture

`index-divergence-1.5.6.duckdb.gz` contains only the `fatal_fixture` table and its unique index, created by `generate.go` with duckdb-go `v2.10506.0` (DuckDB `v1.5.6`). Its 2,048 rows use `server-a`, `STATSZ`, and `ok`. It contains no application data or configuration.

The generator checkpoints 1,750 rows, appends 298, and closes with shutdown checkpointing disabled. A separate process replays the WAL and closes without an explicit checkpoint. DuckDB 1.5.6 loses the appended index entries during that shutdown. The compressed file contains the resulting database, with no WAL. The decompressed SHA-256 is `6fc75f7c185ae346453b104817cd82ac67b90dc7fcb1a40daefc6cf03220f59b`.

The containment tests decompress a fresh copy in a test temporary directory. DELETE succeeds but COMMIT returns a real typed fatal error, including under DuckDB 2.0.0. This fixture tests containment of pre-existing divergence. `TestIndexSurvivesWALReplayShutdown` separately tests prevention using fresh files and the linked engine.

To regenerate, build `generate.go` with a separate Go module pinned to duckdb-go `v2.10506.0`. The generator refuses other engine versions. Run these commands against a new disposable path after building the generator as `/tmp/natslens-index-prevention/generate`:

```sh
/tmp/natslens-index-prevention/generate create /tmp/natslens-index-prevention/regenerated.duckdb
/tmp/natslens-index-prevention/generate replay /tmp/natslens-index-prevention/regenerated.duckdb
```

Both processes print `v1.5.6`. Compress the closed database with gzip and `mtime=0`. Database metadata can differ between generations, so compare the synthetic schema and rows and rerun both containment tests before replacing the fixture.

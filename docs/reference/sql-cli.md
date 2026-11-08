# Offline SQL CLI

`natslens sql` is an advanced local tool for trusted SQL against an existing offline DuckDB file. It needs no collector, NATS connection, credentials or network connection for database-only queries. The proposed semantic investigation interface provides domain commands without requiring SQL or schema knowledge.

The command choices below are proposed, not yet confirmed as the shared investigation/offline CLI contract. The general investigation candidate is remote-only and under review. Bundle acquisition and an interchangeable offline semantic backend are separate work; this command implements neither.

## Source selection

`-db FILE` selects one existing database file. It is exclusive with `-config`, `-system` and `-data-dir`. Legacy directory and configured-system selection remain available: `-data-dir` overrides the configured directory, otherwise `-config FILE -system ID` selects a system, otherwise the directory is `./data`. A multi-system configuration requires `-system`.

The source resolves to an absolute path with symlinks resolved. It must be a regular file. The pinned Go driver takes its database filename directly from the DSN before `?`, without URL decoding. Filenames containing `?`, `#` or `%` are rejected rather than interpreted as connection settings or silently selecting another file. Spaces, quotes, Unicode and `&` are literal filename characters. Use a safely named, validated offline copy when a source name is unsupported.

SQL opens the file with `access_mode=read_only`. It does not call the store initializer, run migrations, change schema checksums, checkpoint the database or remove a WAL. A missing file is an error, never a request to create a database. An arbitrary DuckDB database can be inspected; a nats-lens schema is not required.

A live collector holds a writer lock, so a separate SQL process cannot inspect that live file. A lock, incompatible DuckDB storage version or invalid file produces `source_unavailable`. The error omits backend text and does not distinguish those causes reliably. Select a snapshot validated by the acquisition workflow; do not try repairs on the source.

Consistent snapshot acquisition is pending in [Investigation workflows](../plans/investigation-workflows.md). Its requirements include user-approved acquisition, a consistent database/WAL generation, compatible engine/storage versions, integrity checks on a disposable copy and provenance identifying the selected generation. Copying an actively written database file alone is not a validated snapshot. This command does not obtain a snapshot, stop a collector or certify the consistency of a supplied file.

## Output selection

The table remains the default, with aligned column labels, `NULL`, UTC times to milliseconds and a final `(N rows)` line. Its temporal rendering retains the legacy driver presentation; explicitly cast special temporal values to VARCHAR when their exact text is needed. `-format json` emits one compact JSON document followed by a newline. `-output FILE` publishes either format to a new file instead of stdout. Flags precede the single SQL argument.

For a selected offline file, a schema-independent query is:

```sh
natslens sql -db snapshot.duckdb -format json 'SELECT 9223372036854775807::BIGINT AS id, NULL::VARCHAR AS label'
```

The document has `version: 1`, `source`, `columns`, `rows`, `truncated` and `limits`. `source` records `kind: "database"`, the canonical `path`, `readOnly: true` for the selected database and `snapshot: "unverified; caller-selected offline file"`. It does not claim an acquisition identity, collection pin, system, observed interval, coverage, retention, freshness or redaction. SQL can bypass semantic response redaction; only query trusted data with authorization to read every selected value.

`columns` is an ordered array of `{ "name": "…", "type": "…" }`. `type` is the pinned driver's DuckDB logical type name, including decimal width/scale and nested element types where exposed. `rows` is an array of arrays in exactly that column order. Duplicate labels remain unchanged and occupy separate positions. Empty results have `rows: []` and retain the column metadata. JSON objects keyed by column label would lose duplicate columns and are not used.

## JSON value representation

The encoding applies recursively to nested values:

| SQL value | JSON representation |
|---|---|
| NULL | `null`, distinct from zero, false, empty text, empty BLOB and an empty list. |
| Signed/unsigned integers, including HUGEINT | Decimal strings at every width; no floating-point conversion. |
| DECIMAL | Exact decimal string; trailing fractional zeros can be omitted. Width and scale remain in `columns[].type`. |
| Finite FLOAT/DOUBLE | JSON number; the original SQL floating-point approximation remains. |
| NaN, positive/negative infinity | Strings `"NaN"`, `"+Inf"`, `"-Inf"`. |
| BOOLEAN | JSON boolean. |
| VARCHAR/ENUM | JSON string. Invalid UTF-8 values fail rather than being silently replaced. |
| BLOB | Standard padded base64 string, including an empty string for an empty BLOB. |
| LIST/ARRAY | Ordered JSON array with recursive encoding and null elements retained. |
| MAP | Ordered array of `[key, value]` pairs, preserving non-string keys and driver entry order. |
| STRUCT | Object of field names with recursively encoded values; object-key order has no meaning. |
| UNION | Object with `tag` and recursively encoded `value`. |
| INTERVAL | Object with decimal strings `months`, `days` and `micros`; no conversion to a fixed duration. |
| UUID / BIT | UUID bytes as standard padded base64 (the pinned driver returns 16 bytes); BIT as a bit string. |
| DATE/TIME/TIMESTAMP variants | Finite driver `time.Time` values representable as RFC 3339 in UTC, retaining up to nanoseconds. Unfaithful or out-of-range values fail with `unsupported_value`. |

JSON rejects temporal infinities and UTC years outside 0000–9999, including values inside lists, arrays, maps, structs and unions. It does not replace them with finite-looking dates. The pinned driver converts DATE and timestamp infinities into ordinary `time.Time` values; most are outside RFC 3339's year range. TIMESTAMP_NS infinities instead resemble dates in 1677 and 2262. The CLI checks their exact nanosecond ticks with the pinned native finiteness API and the declared logical type, rather than guessing from a date's appearance. Adjacent finite TIMESTAMP_NS values remain valid. These checks do not label every rejected out-of-range finite value as infinity.

An explicit cast such as `CAST(TIMESTAMP '-infinity' AS VARCHAR)` preserves the special value as `"-infinity"`. Cast a nested member before constructing its list, map, struct or union. The CLI does not insert casts or rewrite arbitrary SQL.

For a timestamp without a time zone, `Z` is a serialization convention for the returned wall-clock fields, not evidence that the original timestamp identified a UTC instant. DATE and TIME retain the driver's synthetic time/date components. TIMESTAMPTZ represents a UTC instant; DuckDB does not preserve the original zone name or offset. SQL JSON columns, including JSON inside lists, arrays, maps, structs and unions, fail with `unsupported_type`. The pinned driver otherwise rounds JSON numbers through `float64` and collapses duplicate object keys. Explicitly cast JSON values to VARCHAR or BLOB in the SQL to preserve the original JSON text or bytes; the CLI never inserts that cast. Unsupported driver values or conversion errors fail the whole result. SQL type metadata distinguishes strings used for binary, numeric and temporal values.

## Budgets and completion

| Flag | Default | Allowed values |
|---|---|---|
| `-max-rows` | 1000 | 1–100000. |
| `-max-output-bytes` | 1048576 (1 MiB) | 1024–16777216 (16 MiB), including metadata and final newline. |
| `-timeout` | 45 seconds | Greater than zero and at most 45 seconds. |

The CLI passes the SQL unchanged. Multiple statements retain the pinned driver's behavior: earlier statements execute and the final result is presented. The row budget reads up to one extra row to establish truncation. `truncated: true` means additional rows existed, not that the SQL itself contained a limit. Table mode adds `(truncated: row budget reached)`. An exact-boundary result is not marked truncated. Ordering comes from the SQL; no implicit `ORDER BY` or `LIMIT` is added. No continuation token or automatic second query is provided.

Byte overflow fails the command, rather than emitting a shortened success document. JSON serialization and all row conversion finish before output. Query, conversion, cancellation and limit failures publish no stdout data or destination file. `-output` uses a private temporary file in the destination directory and atomically links it into place; the destination must not exist, including symlinks or hardlinks. It never replaces the source. It removes its temporary file on normal error paths; a killed process can leave a staging file. Publication is atomic to observers, not a guarantee of survival across power loss.

Data goes to stdout; diagnostics go to stderr. Success and help exit 0; failures exit 1. A stdout device or pipe failure can transmit a prefix, so consumers must require exit 0 and parse the entire document. Use `-output` for atomic file publication. Shell redirection can create an empty file on failure and does not have the `-output` contract. There is no NDJSON or streaming mode.

SIGINT/SIGTERM cancel through the process context. The driver receives that context for SQL execution and the CLI checks it while reading and before returning prepared output. The timeout includes source validation, opening, querying, row conversion and encoding; the native database open is synchronous and cannot be interrupted mid-open. File publication/stdout writes are outside that query budget and can block. Cancellation and deadlines are cooperative, not a hard wall-clock or resource sandbox. A query can materialize its result inside DuckDB before row/output limits apply; these flags do not bound native memory, CPU, temporary disk use or external side effects.

Diagnostics are single JSON objects with `version: 1` and `error.code`, `error.message` and `error.nextStep`. SQL diagnostics use the candidate investigation envelope shape, but use stderr and more specific codes; the remote candidate's error/output-file conventions are not a confirmed parity contract.

| Code | Meaning |
|---|---|
| `invalid_argument` | Invalid flags, budgets or source selection. Configuration-resolution failures use a fixed message and safe next step; values, paths and configured system IDs are omitted. |
| `source_unavailable` | Missing source, read-only open failure, live lock, incompatible version or invalid file. |
| `query_failed` | SQL execution, scan or serialization failure; raw backend error and SQL text are omitted. |
| `unsupported_type` | A scalar or nested JSON alias would lose precision or duplicate object keys through the pinned driver. |
| `unsupported_value` | A temporal infinity or a UTC year outside RFC 3339 bounds cannot be represented faithfully; explicitly cast the value to VARCHAR. |
| `cancelled` | Context cancelled or time budget expired. |
| `output_limit` | Complete output exceeds the byte budget. |
| `output_failed` | Destination publication or stdout write failed. |

## Trusted SQL boundary

Read-only access protects the selected database from ordinary writes. It is not an arbitrary-query sandbox. The command preserves the driver's external capabilities and does not apply a keyword filter or rewrite arbitrary SQL. A failed command or discarded output does not roll back external SQL side effects.

Disposable synthetic fixtures against `github.com/duckdb/duckdb-go/v2 v2.20000.0` (DuckDB 2.0) establish that:

- A database-only SELECT works with no nats-lens tables, collector or network.
- Writes to the selected database fail, and ordinary inspection leaves the source bytes and directory entries unchanged.
- `read_csv` reads a separate local fixture file and `COPY (SELECT …) TO …` writes a separate local file.
- `ATTACH … (READ_WRITE)` creates a separate database and SQL can write to it, even though the primary database is read-only.
- `enable_external_access`, `autoload_known_extensions` and `autoinstall_known_extensions` are enabled; `allow_unsigned_extensions` is disabled in the pinned build.

No extension was installed, no extension worker changed, and no network query was executed in this assessment. Signed extension installation/loading and supported remote file access remain part of the trusted SQL threat boundary. The exact outcome depends on available extensions, filesystem permissions and network access; read-only mode does not deny those capabilities. DuckDB's [security model](https://duckdb.org/docs/2.0/operations_manual/securing_duckdb/overview) treats untrusted SQL as code, and its [extension security reference](https://duckdb.org/docs/2.0/operations_manual/securing_duckdb/securing_extensions) describes extension privileges. Configuration restrictions alone are not an OS sandbox.

Only execute SQL whose complete behavior is trusted, under the operating-system permissions appropriate for the selected data. Raw SQL is not exposed by the remote investigation interface, the default agent surface or MCP. Offline bundles, semantic backend parity, acquisition manifests, untrusted execution and automatic anonymization are out of scope.

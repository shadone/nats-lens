# Acquire an offline dataset

Use the collector's opt-in local administrative channel to acquire one system while collection continues. The result stays on the collector's filesystem until you explicitly transfer it. Use trusted artifacts only: checksums detect corruption, not a malicious author or database.

## Prepare the administrative channel

The collector must already be configured through your deployment's normal access controls. Enabling these flags requires an explicit operator-controlled collector start; the acquisition command does not restart a collector. The mechanism and command surface remain proposed, not yet confirmed as owner policy.

Create separate private directories owned by the collector user for the socket and exports. Where hard disk isolation is required, place exports on a filesystem with an OS-enforced quota. The application does not install or verify quotas. Leave capacity on the source filesystem too: a pinned read transaction can retain MVCC/WAL history. Native work, checkpointing, and cancellation are cooperative.

1. Create a private directory for the socket and a private export directory, each with mode 0700. Keep the socket path short enough for the platform's Unix address limit.
2. Add `-acquisition-socket <socket-path>` and `-acquisition-dir <export-directory>` to the collector's next authorized `run` invocation. Optionally set `-acquisition-max-bytes <bytes>`; the default success budget is 1 GiB and the maximum is 4 GiB.
3. Verify that the collector started without an acquisition configuration error and that the socket has mode 0600. The ordinary HTTP API remains separately controlled by the deployment's existing proxy/access configuration.

The socket grants administrative access to the local collector user. It is not an agent endpoint or a remote connection. Do not expose the socket or export directory through a public listener or tunnel as part of this procedure.

## Request and validate a dataset

1. On the collector host or inside its container, run the acquisition command with the configured socket and system ID:

   ```text
   natslens acquire -socket <socket-path> -system shop-prod
   ```

   A successful response contains `path` and `manifest`. `path` names a new directory on that host/container, never an automatic download to your client. Failure reports a busy, space, budget, cancellation, invalid-request, or acquisition error and exits unsuccessfully.

2. Inspect the manifest's scope, collection reference, retained history, event bounds, and pending-stage statements. The scope is the full retained database for one system, not a filtered account/resource export. Missing monitoring samples and in-memory buffers do not become complete because the artifact validated.
3. Open the trusted artifact through the offline consumer's shared dataset validator before querying it. For advanced raw SQL after validation, select its closed `data.duckdb` with the [SQL CLI](../reference/sql-cli.md). Use a matching schema/engine binary; acquisition never migrates or repairs the source.

Copying only a running collector's database file is not a supported snapshot method. A separate process cannot inspect that active writer's file read-only. This procedure uses the collector's own engine. If the administrative channel is unavailable, schedule an authorized stop and preserve the complete matching database generation and sidecars under the [database recovery procedure](database-recovery.md); do not raw-copy an open file.

## Transfer and retain the artifact

1. Choose an authorized destination and explicit transfer tool. Transfer the entire completed directory, including both files, over your approved secure channel. Preserve directory mode 0700 and file mode 0600.
2. Validate the copied directory with the shared offline reader. Compare its SHA-256 and size with the source manifest. Treat the manifest and database as untrusted until their origin is independently trusted; matching hashes alone do not establish that origin.
3. Retain the complete artifact according to your evidence policy. Dataset retention is independent of live database retention and hourly archive retention. Acquisition never automatically deletes or uploads completed artifacts.
4. Delete only explicitly selected artifacts when your evidence retention expires. If a killed export left `.dataset-partial-*`, first verify that no acquisition still uses that exact directory. The public reader rejects that reserved staging name even if its prepared manifest says `complete: true`. Restart neither publishes nor deletes it.

For a stale socket after a crash, verify that its collector is no longer listening before explicitly removing that socket and restarting through normal deployment controls. Startup refuses an existing socket rather than unlinking it. Existing completed artifacts remain valid across restart.

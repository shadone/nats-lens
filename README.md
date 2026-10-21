# nats-lens

nats-lens observes a NATS deployment and records what every server reports about itself, its connections and its JetStream assets, so that an operator can answer what failed, when, on which server, and what else happened at the same time. It runs SQL checks over what it recorded, keeps the findings with their history, and shows it all in a web UI embedded in one binary. It is read-only towards NATS.

**Status:** early development. The configuration, the API and the database schema may still change between versions.

## Quick start

Build the binary with the web UI (Go 1.27 and Node.js 22 or later; see [Prerequisites](#prerequisites)):

```sh
git clone https://github.com/shadone/nats-lens.git
cd nats-lens
make build
```

See it without a NATS system: `natslens demo` serves three fixed systems (a three-server cluster after a Kubernetes node upgrade, a 24-server supercluster and a staging system that stopped collecting) on http://127.0.0.1:8080.

```sh
bin/natslens demo
```

Point it at a real NATS system: give it a user of the system account (see [NATS permissions](#nats-permissions) for the grants it needs) and open http://127.0.0.1:8080. It stores its data in `./data`.

```sh
NATSLENS_NATS_PASSWORD=... bin/natslens run -nats-url nats://nats.example.com:4222 -nats-user natslens
# or with a credentials file
bin/natslens run -nats-url nats://nats.example.com:4222 -nats-creds ./sys.creds
```

To capture the stream and consumer advisories of a JetStream account as well, and for every other setting, use a configuration file: [natslens.example.yaml](natslens.example.yaml) lists every key. To run it in Kubernetes, see [Deploy](#deploy).

- **Design** is in [docs/design.md](docs/design.md).
- **Database schema** is in [internal/store/migrations/](internal/store/migrations/); every table and column carries a comment.
- **License**: Apache-2.0, see [LICENSE](LICENSE).

## Key components

- Collector: connects to the monitored system's system account and takes one snapshot (an epoch) per interval. It discovers the servers with `$SYS.REQ.SERVER.PING.STATSZ`, then requests VARZ, CONNZ, ROUTEZ, GATEWAYZ, LEAFZ, ACCOUNTZ, account STATZ, JSZ, HEALTHZ, RAFTZ (the system account and each JetStream account) and IPQUEUESZ from every server, with a collect window per request.
- Every server's own view is kept: an R1 asset appears only on its host, and disagreement between servers is visible in the data.
- Each epoch records the servers it expected and the ones that answered, and the outcome of every request per server. An epoch with a missing server or endpoint is stored and marked partial.
- Events: subscriptions record what a snapshot misses. From the system account: client connects and disconnects with their reason (for example a slow consumer), authentication errors, account connection counts, lame duck and shutdown, and the JetStream advisories that concern no account (meta leader elected, out of storage, API limit reached). From each JetStream account in `accounts`, with its own credentials: stream created, updated and deleted and consumer created and deleted, with the client that made the call (nats-server 2.15 sends no advisory for a consumer update); leader elected; quorum lost; max deliveries; NAK; terminated; pause; sampled acks.
- The collector records itself as well: a `collector.gap` event on start covering the time since the last stored epoch or event, and events for losses and reconnects of its own connections, for a connection without a server for 30 seconds (`collector.not_connected`), and for dropped messages, so that silence is never read as health.
- Kubernetes (optional, `kubernetes.enabled`): a read-only watcher records the Events of the NATS namespace, the NATS server pods (scheduled, ready, not ready, restarted, evicted, terminating, deleted) and the nodes they run on (cordoned, drained, ready, not ready, upgraded, deleted) as events with source `k8s`, each pod event named by its NATS server. The timeline draws them in its Kubernetes lane and shades node drains and upgrades as windows. See [Kubernetes](#kubernetes).
- Storage: an embedded DuckDB file, one transaction per epoch, events written in batches (every second or 500 events), versioned migrations, and a retention sweep (14 days by default) for epochs and events.
- Archive: every table's rows for an hour go to `archive_dir/<table>/date=YYYY-MM-DD/hour=HH.<export>.parquet`, listed by a manifest in `archive_dir/manifests/` that is written last. Every export of an hour writes new file names, and files of earlier exports of that hour are removed after the new manifest once they are 10 minutes old, so a crash never mixes two exports. The archive directory expects one natslens writing to it at a time; two that overlap for a while (during a rollout) leave unused files behind but never break a rebuild. The open hour is written provisionally every 5 minutes and finally once closed; an hour whose rows change later is written again, and missed hours are caught up after downtime. The `mutes` table is written whole with every hour, the newest hour is written again when a mute is created or lifted, and a rebuild keeps each mute's newest copy (the lifted one once it was lifted). `archive_dir/state.json` holds the highest epoch, event and mute numbers. When the database file is missing or empty on start and the archive has complete hours inside the retention window, the database is rebuilt from them. natslens checkpoints the database after applying migrations, so that a crash right after start leaves a file DuckDB can open. A database whose write-ahead log DuckDB cannot replay is never deleted: on start the log and the file are renamed to `<name>.corrupt-<UTC time>`, the database is rebuilt from the archive, an error is logged, and a `collector.database_recovered` event names the kept files. After a successful export, at most once an hour, whole hours that ended more than `archive_retention` ago (90 days by default; 0 keeps every hour) are removed, data files first and the manifest last.
- API: `natslens run` serves the Connect service `natslens.v1.LensService` under `/api/` (Connect, gRPC and gRPC-Web, HTTP/2 without TLS for gRPC), `/healthz` (the process serves), `/readyz` (every system's database is open, any rebuild finished, and the API serves), and the web UI at `/`. It has no authentication of its own and binds to localhost by default; deploy it behind IAP and set `iap_audience`, so that mutes record the verified IAP user. Every response carries a Content-Security-Policy (the page's own files only, the inline theme script by its hash, no framing), `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer`. The protobuf is in [proto/natslens/v1/](proto/natslens/v1/); generated Go code is in `internal/gen`, TypeScript for connect-es in `web/src/gen`. The API serves the findings with their transitions, the status timeline, events and mutes. `GetTimeSeries` reads one of the metrics `ListMetrics` lists, each with its unit and what a bucket combines; `cluster.cpu_percent` and `cluster.mem_bytes` are bands, whose points carry the median server as the value and the lowest and highest server as `low` and `high`, with the busiest server over the range in `top` and its own series in `top_points`. A cluster metric with an entity is `invalid_argument`. `GetTimeSeriesBatch` reads up to 32 such series over one range in one call, each with its own result or `error`. `ListEvents` takes four optional flags; without them the response is as before. `hide_routine` leaves out routine events (the kinds `client.churn`, `consumer.ack_sampled` and `account.connections`; `api.audit` records of successful calls that change nothing; and connects and disconnects of tooling clients as the client checks judge them, without `checks.include_tooling`, since hiding only changes what is shown), counts them in `routine_hidden` and lists them by kind and client in `routine_groups`. `fold_repeats` folds events with the same source, kind, server, account, stream, consumer, client name and reason (and finding, or Kubernetes object), each at most 10 minutes after the previous one, into one row with `repeats` and `first_time`; its pages keep to the events of the first page. `total_count` and the facet counts count events, not rows. `include_facets` adds the counts per source and per server, each without its own filter, and `total_unfiltered`. `histogram_buckets` (at most 120) adds warning, notable and routine counts per bucket of the range. Every request except `ListSystems` and `ListMetrics` takes a `system`, the ID of the system to read; empty reads the default system, the first configured, and an unknown ID is `invalid_argument` naming the known ones. `ListSystems` lists the systems in configuration order with a summary of each from data already collected: the collection state and its error, the newest epoch's age, server, cluster, stream and connection counts (unset when the range holds no epoch), the worst finding and the tally, and a strip of the worst status per bucket of the range (48 buckets unless `strip_buckets` says otherwise, at most 500). It reads the systems in parallel, each within 5 seconds; a system that fails or runs out of time is listed with an `error` and the others still return. Calls for the same range and strip that arrive while a system's summary runs share that run. The collection state and error come from the collector of the running process; without one, from the newest `collector.connection_closed` or `collector.not_connected` event of the collector's connection after the newest epoch and reconnect. The freshness of `GetOverview` and of each summary says whether the system's collector collects (`collecting`, false while its connection is not connected or closed, with `not_collecting_since`), from the same collection state. `ListAccounts` lists a system's accounts with their connections, streams and consumers. `GetServer` returns a server's routes, gateways and leaf nodes, its limits with those that differ from most servers', and, from the Kubernetes watcher's events, its pod and node. `ListKeyValueStores` and `ListObjectStores` list a system's KV buckets and object stores from their `KV_` and `OBJ_` streams, as the leader reports them in the newest collection of the range, worst status first, with the buckets the inventory lists that no server reports. `natslens run` serves the configured systems, a file without `systems` as one system with the ID `system_id` (default `default`); `natslens demo` serves `example-prod`, `example-global` and `example-staging`. The web UI sends the `?system=` of its URL with every call, and keys its cached answers by it.
- Checks: after every stored epoch, natslens runs the checks in [checks/](checks/), one SQL file per check, and keeps the result as findings: one per check and failing entity, with its transitions (firing, changed severity, resolved) and whether it flaps (more than three firing-and-resolving cycles in an hour). See [Checks](#checks).
- Inventory: an optional file lists the streams, durable consumers, KV buckets and object stores each account should have, with the settings that matter; the inventory checks report what is missing or set otherwise, and name the client that deleted it when the delete advisory was captured.
- Demo: `natslens demo` serves the API without NATS or a database, with three fixed systems: `example-prod` (the default; the reference mockup's cluster after a GKE node upgrade), `example-global` (24 servers in clusters `eu`, `us` and `ap` joined by gateways, one server down after a node upgrade, one store 71% full, two servers on an older version) and `example-staging` (watched for nine hours and not collecting: its credentials were rejected three hours ago, and its data ends there; its history begins inside the default range).
- Web UI: React, TypeScript and TanStack Query in [web/](web/), built with Vite into `internal/webui/dist` and embedded into the binary with `go:embed`. It follows the design in [docs/ui-reference/reference.html](docs/ui-reference/reference.html) in a light and a dark theme, self-hosts its fonts and makes no third-party requests. A sidebar lists the pages (in `web/src/nav.ts`) under a system switcher; the header holds the account scope, a search by name and the theme menu, and the sidebar becomes a drawer at phone width. Fleet shows a card per system, worst first, with its state, headline, counts, status strip and freshness, and opens its Overview; with several systems `/` lands there, and its sidebar badge counts the systems that are not OK. The Overview leads with a one-line status, then nine metric boxes, the firing findings grouped by cause with the muted ones and their reasons, eight trend charts, timeline lanes for Kubernetes, findings and events, the busiest consumers and the streams with their placement. Events opens on the events worth reading, with routine events hidden behind a count and repeats folded into one row, a histogram of warnings, notable and routine events, counts per source and server, and new events held behind an "N new" pill. A finding's page gives when its condition began and when nats-lens first saw it, each server's copy of its stream or consumer, and the events around its start. Every page reads the time range (`?range=1h|6h|24h|7d` live, or `?from=&to=` fixed), the system (`?system=`) and the account scope (`?account=`) from the URL, and links between pages carry all three, so links are shareable. Pages read view models from `web/src/api`, the only code that touches the generated client.

- Go command `natslens` built from [cmd/natslens](cmd/natslens)
- Decodes replies into the monitoring types exported by the `nats-server` Go package
- Connects with the NATS Go client
- DuckDB through the official `duckdb-go` driver
- Logs JSON with `log/slog`; credentials are never logged

## Development

### Prerequisites

- Go 1.27 (1.27.2 or later for the standard library's security fixes)
- `nats-server` (v2.15) and the `nats` CLI, for the local dev cluster
- Node.js 22.22, 24.15 or 26 and npm, to build and test the web UI (`make build` runs `make web`)
- `buf`, only to regenerate the API code (`make proto`)

### Setup

1. **Clone the repository**

2. **Build**

   ```sh
   make build
   ```

3. **Start a local cluster, create assets and collect**

   ```sh
   make dev-cluster
   make load
   make run-dev
   ```

   `make run-dev` collects every 10 seconds into `.dev/natslens/natslens.duckdb`, with events from the system account and the APP account, until stopped with Ctrl-C. Its configuration is [scripts/natslens.dev.yaml](scripts/natslens.dev.yaml). In a second terminal, `make churn` deletes a consumer from a connection named `load-churn`, waits 20 seconds (two collect intervals; `CHURN_WAIT` changes it), recreates it and restarts nats-2.

4. **Query the database** once the collector has stopped (DuckDB lets one process hold the file)

   ```sh
   bin/natslens sql -data-dir .dev/natslens "SELECT epoch, observed_servers, missing_names, partial FROM epochs ORDER BY epoch"
   bin/natslens sql -data-dir .dev/natslens "SELECT time, kind, server_name, stream, consumer, client_name, summary FROM events ORDER BY id"
   ```

5. **Call the API**, from `make run-dev` or, without a cluster, from `make demo`

   ```sh
   curl -s -H 'Content-Type: application/json' -d '{}' http://127.0.0.1:8080/api/natslens.v1.LensService/GetOverview
   ```

6. **Work on the web UI** with hot reload: run `make demo` (or `make run-dev`) in one terminal and `make web-dev` in another, then open http://127.0.0.1:5173. The Vite dev server forwards `/api` to 127.0.0.1:8080; set `NATSLENS_API` to use another address. `make web-test` runs the Vitest tests. `go build` alone embeds a placeholder page instead of the UI; `make build` builds both.

### Useful commands

| Command               | Description                                                                         |
| --------------------- | ----------------------------------------------------------------------------------- |
| make build            | Builds the web UI, then `bin/natslens` with the UI embedded.                       |
| make build-go         | Builds `bin/natslens` alone, with a placeholder page instead of the UI.            |
| make web              | Builds the web UI into `internal/webui/dist` (`npm ci` first when the lockfile changed). |
| make web-dev          | Runs the Vite dev server on 127.0.0.1:5173 against the API on 127.0.0.1:8080.       |
| make web-test         | Runs the web UI tests (Vitest and Testing Library).                                 |
| make test             | Runs all tests, including the in-process 3-node cluster test.                      |
| make test-short       | Runs the tests without the cluster test.                                            |
| make lint             | Runs `go vet` and checks `gofmt`.                                                   |
| make dev-cluster      | Starts nats-0, nats-1 and nats-2 on localhost (`scripts/dev-cluster.sh start`).     |
| make dev-cluster-stop | Stops the dev cluster. `scripts/dev-cluster.sh stop 2` stops only nats-2.           |
| make load             | Creates the example shop's streams, consumers and KV buckets and publishes traffic. |
| make churn            | Deletes a consumer from a named connection, recreates it 20s later, then restarts nats-2. |
| make run-dev          | Runs the collector against the dev cluster.                                         |
| make demo             | Serves the API with the three demo systems on 127.0.0.1:8080, without NATS.         |
| make proto            | Lints the protobuf and regenerates the Go and TypeScript code.                      |
| make fmt              | Formats the Go code with `gofmt -w`.                                                |
| make image            | Builds the linux/amd64 container image with buildx; `IMAGE` names it (default `natslens:dev`). |
| make vuln             | Runs `govulncheck ./...`; fails with an install hint when govulncheck is missing.  |
| make clean            | Removes `bin/`.                                                                     |

The dev cluster uses client ports 4222-4224, cluster ports 6222-6224 and monitoring ports 8222-8224. Its system account user is `sys` and its JetStream account user is `app`; the passwords are local development values in [scripts/dev-cluster.sh](scripts/dev-cluster.sh). Configuration, logs and data live in `.dev/`, which git ignores.

### Commands

| Command                              | Description                                                   |
| ------------------------------------ | ------------------------------------------------------------- |
| natslens [run] [flags]               | Takes one epoch per interval of every configured system, captures events and archives closed hours until stopped. |
| natslens once [-system ID] [flags]   | Takes and stores a single epoch of every system, or only of the system `-system` names; no events. |
| natslens sql [-config FILE] [-system ID] [-data-dir DIR] QUERY | Runs a query against a database no collector has open: `-data-dir`, else the data directory of the system `-system` names (required when the file lists several), else `./data`. |
| natslens checks list [-config FILE]  | Lists every check with its entity, severity, kind, lookback and thresholds after the file's overrides, and whether it is disabled. |
| natslens checks run [-config FILE] [-system ID] [-data-dir DIR] [-epoch N] | Evaluates every check for the newest (or the given) epoch of a database no collector has open, read-only, and prints what fails; stores nothing. The database is chosen as for `natslens sql`. |
| natslens demo [-listen ADDR] [-allowed-host H] [-fixed] | Serves the API with the three demo systems, shifted to now unless `-fixed`. |

## Configuration

Configuration comes from defaults, then an optional YAML file (`-config`), then flags. [natslens.example.yaml](natslens.example.yaml) lists every key.

| Key                   | Flag             | Default                 | Description                                              |
| --------------------- | ---------------- | ----------------------- | -------------------------------------------------------- |
| `interval`            | `-interval`      | `30s`                   | Time between collections.                                |
| `timeout`             | `-timeout`       | `3s`                    | Collect window for one request; under a third of `interval`. |
| `data_dir`            | `-data-dir`      | `./data`                | Directory for `natslens.duckdb`.                         |
| `retention`           | `-retention`     | `14d`                   | Epochs older than this are deleted.                      |
| `server_forget_after` | `-server-forget-after` | `1h`              | How long a server that stopped answering is expected; must be positive. |
| `connz_limit`         | `-connz-limit`   | `4096`                  | Most connections stored per server per collection.      |
| `subscriptions_interval` | `-subscriptions-interval` | `5m`         | Shortest time between two collections of the servers' subscription lists (SUBSZ), taken with a collection; `0` turns them off. |
| `subsz_limit`         | `-subsz-limit`   | `10000`                 | Most subscriptions stored per server per subscription collection. |
| `shutdown_grace`      | `-shutdown-grace` | `5s`                   | How long a collection's write may continue after a stop signal; positive and at most `interval`, since a write never runs longer than one interval. |
| `log_level`           | `-log-level`     | `info`                  | `debug`, `info`, `warn` or `error`.                      |
| `system_id`           |                  | `default`               | Id of the one system a file without `systems` describes: lowercase letters, digits and `-`, at most 63, starting with a letter or digit. Used in URLs, the API and metric labels. |
| `systems`             |                  |                         | Several NATS systems in one natslens; see [Several systems](#several-systems). |
| `nats.url`            | `-nats-url`      | `nats://127.0.0.1:4222` | System account URL; a comma-separated list is allowed.  |
| `nats.name`           |                  | `natslens`              | Connection name the servers show; the event connection adds `-events`, an account connection `-<account>`. |
| `nats.creds`          | `-nats-creds`    |                         | Credentials file for the system account.                 |
| `nats.user`           | `-nats-user`     |                         | System account user.                                     |
| `nats.password`       |                  |                         | Password; read from `NATSLENS_NATS_PASSWORD` when `nats.user` is set and the file has none. |
| `nats.tls.ca`         | `-nats-tls-ca`   |                         | CA file for the server certificate.                      |
| `nats.tls.cert`       | `-nats-tls-cert` |                         | Client certificate.                                      |
| `nats.tls.key`        | `-nats-tls-key`  |                         | Client key.                                              |
| `archive_dir`         | `-archive-dir`   | `data_dir/archive`      | Directory for the hourly Parquet files.                  |
| `listen`              | `-listen`        | `127.0.0.1:8080`        | Address of the API and UI in `natslens run`; empty disables them. |
| `allowed_hosts`       | `-allowed-host`  | loopback names on a loopback `listen`, else any | Host header values the API accepts; the flag takes a comma-separated list. |
| `iap_audience`        | `-iap-audience`  |                         | Behind IAP: the backend service's audience. Mutes then need the verified IAP assertion and record its user. |
| `metrics_timeout`     |                  | `8s`                    | How long each system may take to write its part of a `/metrics` scrape; must be lower than the Prometheus `scrape_timeout` (10 seconds by default). |
| `allow_unauthenticated` |              | `false`                 | Serve the API beyond loopback without `iap_audience`; natslens refuses that otherwise. |
| `duckdb_memory_limit` |                  | 60% of the cgroup limit, else 1 GiB | Memory DuckDB may use, with a KiB, MiB, GiB or TiB suffix. |
| `events.enabled`      |                  | `true`                  | Capture events in `natslens run`.                        |
| `archive_provisional_every` |            | `5m`                    | How often the open hour is archived; `0` disables it.    |
| `archive_retention`   |                  | `90d`                   | Archived hours older than this are removed after an export; `0` keeps every hour. At least `retention`. |
| `events.api_audit`    |                  | `false`                 | Also store every JetStream API audit advisory (one per API call). |
| `events.account_connections` |           | `false`                 | Also store the connection count every server sends for every account every 30 seconds. |
| `events.flush_interval` |                | `1s`                    | Longest time an event waits before it is written; at most `5m`. |
| `events.batch_size`   |                  | `500`                   | Buffered events that trigger a write.                    |
| `events.churn.collapse` |                | `true`                  | A connect whose disconnect, with reason "Client Closed", follows within `pair_within` is not stored; one `client.churn` event per client name, client host, account and `window` counts such pairs. A disconnect for any other reason is always stored, with its connect. |
| `events.churn.pair_within` |             | `10s`                   | How long a connect waits for its disconnect; a connect not closed by then is stored. At most `window`. |
| `events.churn.window` |                  | `1m`                    | Span of one `client.churn` event. |
| `accounts`            |                  |                         | JetStream accounts whose advisories are captured; see below. |
| `inventory_file`      | `-inventory-file` |                        | YAML file of expected streams, consumers and buckets ([inventory.example.yaml](inventory.example.yaml)); read on every start. |
| `checks.timeout`      |                  | `5s`                    | Longest one check's query may run; at most `interval`. |
| `checks.disabled`     |                  |                         | Check codes that do not run; their findings resolve. |
| `checks.include_tooling` |              | `false`                 | Count tooling connections in the client checks (`client-001` to `client-005`) and in `server-011`'s client list: nats-lens's own connections (`nats.name`, `<name>-events`, `<name>-<account>`) and clients connected from 127.0.0.1 or ::1, such as a port-forwarded CLI. The API marks them `tooling` either way. |
| `checks.loopback_is_tooling` |          | `true`                  | Clients connected from 127.0.0.1 or ::1 are tooling. When such clients are more than 60% of the connections that are not nats-lens's own over the last 10 epochs (a sidecar that makes inbound traffic look local), they are not, until the share falls below 40%; natslens logs it once. An epoch with fewer than 3 such connections keeps the previous decision. |
| `checks.restart_window` |                | `10m`                   | How long a restarted server counts as restarting: checks that judge the cluster leave its own reports out, and a planned restart is reported at info. |
| `checks.thresholds`   |                  |                         | Threshold overrides: check code to threshold name to value; see [Checks](#checks). |
| `checks.remediation_file` |             |                         | YAML file of extra remediation text per check code, appended to the check's own after a `---` line. Plain text in which `backticks` mark code; no other Markdown. Read at start only: restart natslens to apply a change (in Kubernetes, a changed ConfigMap needs a rollout). An unknown code stops the start. See [Checks](#checks). |
| `kubernetes.enabled`  |                  | `false`                 | Watch Kubernetes in `natslens run`; needs `events.enabled`. |
| `kubernetes.kubeconfig` |                |                         | Kubeconfig file for a run outside the cluster; empty means the in-cluster service account. |
| `kubernetes.context`  |                  |                         | Kubeconfig context; empty means its current one. |
| `kubernetes.namespaces` |                | the namespace natslens runs in | Namespaces whose Events and NATS pods are watched. |
| `kubernetes.nats_selector` |             | `app.kubernetes.io/name=nats,app.kubernetes.io/component=nats` | Label selector of the NATS server pods (the Helm chart's labels). |
| `kubernetes.server_name` |               | `{pod}`                 | NATS server name of a pod: `{pod}` and `{namespace}` are replaced. The Helm chart names each server after its pod. |
| `kubernetes.node_events_namespace` |     | `default`               | Namespace the API server files node Events in. |
| `kubernetes.nodes`    |                  | `true`                  | Watch nodes and node Events; needs a ClusterRole that may read nodes. |
| `kubernetes.pool_nodes` |               | `false`                 | Also keep the changes of every node of a pool that held a NATS node; off for Autopilot. |
| `kubernetes.gap_after` |                 | `30s`                   | How long a watch may fail before it is recorded. |
| `kubernetes.client_pods` |               |                         | Regular expression of pod names that are NATS clients besides those a connection name gives after "@"; their FailedScheduling, BackOff and OOM kill Events are drawn on the timeline's Kubernetes lane as warnings. |

On SIGTERM or SIGINT natslens lets a running epoch write finish for up to `shutdown_grace`. At the same time it drains the event connections, all at once, for at most `events.flush_interval`, and writes the buffered events until `shutdown_grace` plus `events.flush_interval` after the signal; events that do not fit by then are counted in a `collector.messages_dropped` event. An archive export running at the signal is cancelled; natslens waits for it until the same deadline and abandons it there. The last `state.json` is written after the last events, by that deadline or, when the events used it up, within 2 seconds after it. With several systems, every system follows these steps at the same time against the one deadline. The API stops accepting connections at the signal and gives calls in progress up to 5 seconds from then before it closes their connections; the databases close only after the API stopped, all at once, by the same deadline as the last `state.json`. A database still closing then is left to the next start, which replays its write-ahead log. A signal while the databases open (a long rebuild) ends natslens cleanly, with exit status 0. Every other shutdown step ends by `shutdown_grace` plus `events.flush_interval` after the signal (the events' built-in 10-second final wait applies only when no deadline is set, which `natslens run` always sets). So the one rule is: the process manager's stop timeout (Kubernetes `terminationGracePeriodSeconds`, systemd `TimeoutStopSec`) must be longer than `shutdown_grace + events.flush_interval + 2s` or 5 seconds, whichever is longer, plus a second or two for the process to exit. With the defaults (5s and 1s) that is 8 seconds plus the margin, so at least 10 seconds; the Deployment's 30 seconds is enough.

The password has no flag, so that it never shows in the process list. A password written into `-nats-url` (`nats://user:pass@host`) does show there; put it in the file or `NATSLENS_NATS_PASSWORD` instead.

JetStream advisories are published inside the account that owns the asset, so each account needs its own connection. Every entry in `accounts` takes `name` (the account name the servers use, for example `APP`), `url` (defaults to `nats.url`), `creds` or `user` and `password`, and `tls` (the CA defaults to `nats.tls.ca`; the client certificate is never inherited). When an account has a `user` and no password in the file, the password is read from `NATSLENS_ACCOUNT_<NAME>_PASSWORD`, with the name upper-cased and other characters replaced by `_`. The account connection asks `$SYS.REQ.USER.INFO` for the account's name as the servers use it, and events from it carry that name. Until it is known (just after connecting), and for the whole run when the user may not request it or may not receive the reply, they carry the configured `name` instead, so filter on both when the two differ.

### Several systems

`systems` lists the NATS systems one natslens watches. Each entry takes `id` (as `system_id`; ids must differ), `name` (display name, default the id), `environment` (free text shown with the system) and its own `nats`, `accounts`, `inventory_file`, `kubernetes`, `retention`, `archive_retention`, `data_dir`, `archive_dir` and `duckdb_memory_limit`, with the same meaning and defaults as the top-level keys, except:

| System key | Default |
| --- | --- |
| `nats.name` | `natslens-<id>`, so each system's connections can be told apart in CONNZ and server logs; the tooling names follow it (`natslens-<id>-events`, `natslens-<id>-<account>`). |
| `retention`, `archive_retention` | The top-level value. |
| `data_dir` | `<data_dir>/<id>`: the top-level `data_dir` is the root. |
| `archive_dir` | `<archive_dir>/<id>` when the top-level `archive_dir` is set, else `<system data dir>/archive`. |
| `duckdb_memory_limit` | An equal share of what the systems' own limits leave of the top-level `duckdb_memory_limit` (or its default), at least 256 MiB. The systems' total can therefore exceed the budget: when their own limits alone add up to more, or when the 256 MiB floor applies (for example 4 systems without a limit and a 768 MiB budget). natslens logs a warning at start in both cases, naming the floor when it caused it. |

With `systems`, the top-level `nats`, `accounts`, `inventory_file`, `kubernetes` and `system_id` and the `-nats-*` and `-inventory-file` flags are refused, as is an empty `systems`. Two systems may not share directories: an archive directory may not be equal to or inside another system's archive or data directory, and a data directory may not be equal to another's or inside another system's archive directory (symbolic links are resolved component by component, so `..` after a link leaves the link's target; existing directories are compared as files, so on a case-insensitive file system `Data` and `data` match, but two spellings of a directory that does not exist yet are not recognised as one). A data directory inside another system's data directory is allowed. Every other key (interval, timeout, listen, IAP, events, checks, `connz_limit`, `shutdown_grace`) applies to all systems. A system password not in the file is read from `NATSLENS_SYSTEM_<ID>_NATS_PASSWORD` and an account password from `NATSLENS_SYSTEM_<ID>_ACCOUNT_<NAME>_PASSWORD`, with the id and name upper-cased and other characters replaced by `_`; Two connections that would read the same variable, because each has a `user` and no `password` in the file, (for example id `a` with account `b_account_c` and id `a-account-b` with account `c`, or accounts `a-b` and `a_b` of one system) are refused; this also holds for the accounts of a file without `systems`. `NATSLENS_NATS_PASSWORD` and `NATSLENS_ACCOUNT_<NAME>_PASSWORD` apply only to a file without `systems`; when they are set while `systems` is used, natslens logs one warning that names them and ignores them.

A file without `systems` is one system with the id `system_id` whose database and archive stay exactly where they are (`data_dir` and `archive_dir`), so adding `system_id` to an existing file moves nothing. natslens marks each system's data directory with its id on first start (see [Database](#database)): to move an existing single-system file to `systems` and keep its data, list the system with the id it had (`default` unless `system_id` was set) and set its `data_dir` and `archive_dir` to the old directories.

### NATS permissions

natslens only subscribes and makes requests; it never publishes to an application subject. Use a scoped user in the system account rather than the account's full user: a system account user without permissions may also send the `$SYS.REQ` calls that change servers (for example `$SYS.REQ.SERVER.<id>.RELOAD` or kick and lame duck requests). These are the minimal grants; `TestCollectWithScopedSysUser` and `TestCaptureWithScopedUsers` show they are enough, and each test fails when one is taken out.

| User | Publish | Subscribe | Used for |
| ---- | ------- | --------- | -------- |
| System account (`nats.creds`) | `$SYS.REQ.SERVER.PING.>`, `$SYS.REQ.ACCOUNT.PING.STATZ` | `$SYS.ACCOUNT.>`, `$SYS.SERVER.>`, `$JS.EVENT.ADVISORY.>`, `_INBOX.>` | Discovery and the detail requests of every epoch (replies arrive on `_INBOX.>`); connect, disconnect, auth error, lame duck and shutdown events; the advisories that concern no account |
| Each account in `accounts` | `$SYS.REQ.USER.INFO` | `$JS.EVENT.>`, `$SYS.ACCOUNT.CLIENT.AUTH.ERR`, `_INBOX.>` | Stream and consumer advisories and metrics; account-scoped auth errors; the account name lookup and its reply |

In a NATS server configuration (with JWTs, set the same lists on the user with `nsc edit user --allow-pub ... --allow-sub ...`):

```
permissions: {
  publish:   { allow: ["$SYS.REQ.SERVER.PING.>", "$SYS.REQ.ACCOUNT.PING.STATZ"] }
  subscribe: { allow: ["$SYS.ACCOUNT.>", "$SYS.SERVER.>", "$JS.EVENT.ADVISORY.>", "_INBOX.>"] }
}
```

```
permissions: {
  publish:   { allow: ["$SYS.REQ.USER.INFO"] }
  subscribe: { allow: ["$JS.EVENT.>", "$SYS.ACCOUNT.CLIENT.AUTH.ERR", "_INBOX.>"] }
}
```

An account user refused `$SYS.REQ.USER.INFO`, or its `_INBOX.>` reply, keeps the configured label for the run and logs one Info line; nothing else fails.

## Kubernetes

With `kubernetes.enabled`, `natslens run` watches the Kubernetes API alongside NATS. It only reads: it lists and watches Events (`events.k8s.io/v1`) in `kubernetes.namespaces`, the NATS server pods there (by `kubernetes.nats_selector`), the nodes, and the node Events in `kubernetes.node_events_namespace`. Inside the cluster it uses the pod's service account; outside, set `kubernetes.kubeconfig` (and `kubernetes.context`). What it records goes through the event capture, in the same batches as the NATS events, with source `k8s`:

| Kind | From | When |
| --- | --- | --- |
| `k8s.pod.scheduled` | pod watch | A NATS pod is assigned to a node. |
| `k8s.pod.ready`, `k8s.pod.not_ready` | pod watch | Its Ready condition changes; timed by the condition. |
| `k8s.pod.restarted` | pod watch | A container's restart count grows; the reason is the last termination's (for example `OOMKilled`). |
| `k8s.pod.oom_killed` | pod watch, Event | A NATS pod's restart whose last termination is `OOMKilled`. Client pods are not watched, so for them only an Event with reason `OOMKilled` or `OOMKilling` gives it, which kubelets do not send for pods; it is untested on a real cluster. |
| `k8s.pod.evicted` | pod watch, Event | The pod gets a `DisruptionTarget` condition (eviction API, preemption, taint manager, kubelet), or an `Evicted` Event; recorded once. |
| `k8s.pod.terminating` | pod watch | The pod is deleted without an eviction; timed when it was asked to stop. |
| `k8s.pod.deleted` | pod watch | The pod is gone. |
| `k8s.pod.killing`, `k8s.pod.started`, `k8s.pod.unhealthy`, `k8s.pod.backoff`, `k8s.pod.failed_scheduling`, `k8s.pod.preempted` | Event | The kubelet's or scheduler's report of the server container, or a warning about another pod (`CrashLoopBackOff` is a back-off). For a pod that is a NATS client the timeline draws a back-off, failed scheduling or OOM kill as a warning. A Kubernetes Event is one row per UID, updated as its count grows. |
| `k8s.node.added`, `k8s.node.cordoned`, `k8s.node.uncordoned`, `k8s.node.ready`, `k8s.node.not_ready`, `k8s.node.deleted` | node watch | A node changes. A cordon is `spec.unschedulable` or the cluster autoscaler's removal taint. |
| `k8s.node.drained` | derived | A NATS pod is evicted, terminated or killed on a cordoned node; once per cordon. |
| `k8s.node.upgrade` | node watch, Event | A node's kubelet version changes, or a node Event's reason names an upgrade. |
| `k8s.event` | Event | Any other warning, and other reports about a NATS pod. |
| `collector.k8s_not_watching`, `collector.k8s_gap` | the watcher | A list or watch failed for `kubernetes.gap_after`, and the span until it worked again. |

Pod events of a NATS pod carry its server name (`kubernetes.server_name`), so they appear with the server's own events. Node events are kept for nodes that run or ran a NATS pod in the last hour, and for nodes added within 15 minutes of a drain or upgrade (the surge node that ends it); with `kubernetes.pool_nodes` also for every node of a pool that held a NATS node in the last day, which on GKE Autopilot, where the autoscaler's few pools hold every workload, would record the autoscaler's churn. A node Event whose reason names an upgrade counts only for those nodes. Events of other pods are kept when they are warnings or a `Killing`, `Evicted` or preemption, without a server name. Routine pod Events (`Scheduled`, `Pulling`, `Pulled`, `Created`) and those of the config reloader container are not stored. An Event repeats with a growing count; each count is stored once, also across a relist and a restart of natslens.

Each watch resumes from the last resource version it saw, so the server's periodic close of a watch loses nothing; when the version has expired (410) the watcher lists again and records what changed meanwhile, timed when it noticed. A watch the server ends within a second without an error counts as a failure, so it is not reopened in a loop. Lists are paged by 500 objects, at most 20 pages; what is beyond that is left out of the list with a warning in the log, which a NATS namespace never reaches but a cluster with more than 10,000 nodes or a namespace with more than 10,000 Events would.

On start, natslens records a `collector.k8s_gap` from the last stored Kubernetes event or epoch, when that is longer ago than `gap_after`, and compares its first lists with what the stored events last said of each NATS pod (ready or not, present) and node (cordoned or not, present). What changed while it was down is recorded then, marked `observed_at_start` in `raw` and "(observed at start)" in the summary.

The status timeline's Kubernetes lane shows node cordons, drains, readiness, upgrades and deletions, the NATS pods' stops, starts and readiness, and the watcher's gaps; for one server it shows only that server's pods. Windows shade node drains and upgrades: a window opens at a `k8s.node.drained` or `k8s.node.upgrade` and takes in every following node or NATS pod event that comes within 15 minutes of the one before, so it ends at the last node or pod back to ready. A window without a drain or a NATS pod event is not shown. It is labelled "Node upgrade" when it holds an upgrade or nodes of different kubelet versions ("GKE node upgrade" when a node in it carries GKE's `cloud.google.com/gke-nodepool` label), "Node removed by the autoscaler" when every drain in it was the autoscaler's, else "Node drain", with the number of drained nodes and of NATS pods stopped.

## Deploy

### Image

[Dockerfile](Dockerfile) builds the web UI and the binary (cgo, for DuckDB) into `gcr.io/distroless/cc-debian12:nonroot`, running as user 65532, with the base images pinned by digest. The Node and Go stages run on the build machine's platform and the Go stage cross-compiles for the target with Debian's gcc cross toolchain, so an amd64 image builds on an arm64 Mac without emulation (`make image` runs buildx for linux/amd64):

```sh
docker buildx build --platform linux/amd64 --build-arg VERSION=$(git rev-parse --short HEAD) -t natslens .
```

Push it to a registry your cluster can pull from.

### Manifests

[k8s/](k8s/) holds kustomize manifests:

- `k8s/base`: the namespace `nats-lens`, the service account and its read-only RBAC, the volume, the Deployment and the Service. It has no configuration of its own and assumes NATS in namespace `nats`, the NATS Helm chart's labels, and two Secrets with the NATS credentials: `natslens-nats-system` (key `system.creds`) and `natslens-nats-app` (key `app.creds`, for the `APP` account).
- `k8s/components/gke-iap`: a kustomize component for GKE behind IAP: a `GCPBackendPolicy` that turns IAP on for the Service, a `HealthCheckPolicy` that points the load balancer at `/healthz`, a NetworkPolicy that admits port 8080 only from Google's load balancer ranges (35.191.0.0/16, 130.211.0.0/22, which a global external Gateway uses) and from `gke-gmp-system`, and a `PodMonitoring` that has Managed Prometheus scrape `/metrics` every 30 seconds.
- `k8s/examples/basic`: an overlay for any cluster, reached with `kubectl port-forward` only; a NetworkPolicy admits no other pod, which is what lets it set `allow_unauthenticated: true`. That needs a CNI that enforces NetworkPolicy (Calico, Cilium, GKE Dataplane V2); kind's default CNI does not, and there any pod can reach the API and mute findings. Its `kustomization.yaml` lists the steps: create the two Secrets, set the image, `kubectl apply -k k8s/examples/basic`.
- `k8s/examples/gke-iap`: an overlay with the component and an HTTPRoute on an existing Gateway. Placeholders block a deploy until they are filled: the image digest ends in a space, which the API server rejects, and `iap_audience` (`/projects/<project number>/global/backendServices/<id>`, known once the Gateway created the backend service) fails validation, as natslens refuses to listen beyond loopback without it unless `allow_unauthenticated: true`.

Copy an example into your own deployment repository and change what differs; each overlay holds its whole `natslens.yaml`, generated into a ConfigMap whose name changes with the file, so a change rolls the Deployment. For a NATS namespace other than `nats`, patch the namespace of the Role and RoleBinding `natslens-read` and of the pod anti-affinity.

- One replica with the `Recreate` strategy, since DuckDB has one writer; the database and the archive share a PVC (5 GiB in the base, 20 GiB in the `gke-iap` example). The `gke-iap` example gives it `argocd.argoproj.io/sync-options: Delete=false,Prune=false`, so Argo CD never deletes it.
- The pod runs as non-root with a read-only root filesystem and an `emptyDir` on `/tmp`. It carries `cluster-autoscaler.kubernetes.io/safe-to-evict: "false"` and its CPU and memory limits equal its requests (100m and 256 MiB in the base, for a laptop cluster; 500m and 2 GiB in the `gke-iap` example, a starting point for production), which GKE Autopilot needs to accept the annotation; without it the autoscaler's scale-down can evict the single replica several times an hour.
- It prefers a node without a NATS server pod (so a drain of that node does not take the observer away, while it still schedules when no other node fits), and has 30 seconds to stop, more than the shutdown rule under [Configuration](#configuration) needs.
- `/healthz` is its liveness and start probe and the load balancer's health check; `/readyz` is its readiness: true once every system's database is open and the API serves, whatever the state of collection of any system, so the UI stays reachable while NATS is down. The API starts only after the database is open, so during a rebuild from the archive neither endpoint answers; the start probe (`/healthz`, every 10 seconds, 60 failures) allows ten minutes for that. A collector that stopped collecting shows as `collecting` false in the API's freshness, which the UI header shows as "not collecting since 11:30", and is caught by the `nats_lens_last_epoch_age_seconds` alert below, not by readiness.
- The service account may list and watch Events and pods in the NATS namespace, node Events in `default`, and nodes.
- `/metrics` is exempt from `allowed_hosts` on a non-loopback listener, so a scraper that sends the pod IP as Host reaches it; the NetworkPolicy limits who can.

The manifests use a namespace of their own, `nats-lens`, apart from the NATS namespace they read, so that the observer's Secrets, NetworkPolicy and volume stand alone and removing it is deleting one namespace.

### Volume size

The archive holds `archive_retention` (90 days) of hours and the database `retention` (14 days) of epochs and events. Measured on the local dev cluster (6 streams, 6 consumers, 3 connections, 10-second interval) an hour of 30 epochs and 100 events took 150 KB of Parquet, about 5 KB per epoch. For a cluster that reports about 20 streams and 17 consumers per server, assuming 5 to 6 times the local volume per epoch (more streams, consumers, raft groups and connections) and 120 epochs an hour at the 30-second interval, an hour is about 3 to 4 MB, 90 days about 6 to 8 GB, and the database for 14 days 1 to 2 GB. This is an estimate, not a measurement: check `du -sh /var/lib/natslens/*` after the first day. The `gke-iap` example's 20 GiB volume leaves room; the base's 5 GiB suits a small cluster; lower `archive_retention` before the volume fills.

### Running

natslens run keeps running while NATS is unreachable: the API and the Kubernetes watcher go on, epochs are skipped until the connection has a server (recorded as `collector.not_connected` after 30 seconds and `collector.reconnected` once it connects), and the UI header shows "not collecting since" the time it stopped. A collector connection that nats.go closes for good, for example after repeated authorization errors when credentials were revoked or rotated, is recorded as `collector.connection_closed` and dialled again, waiting 2 seconds doubling to a minute, like the event connections; a successful redial is recorded as `collector.reconnected`. DuckDB's memory is bounded by `duckdb_memory_limit`, by default 60% of the container's memory limit (1.2 GiB with the 2 GiB of the `gke-iap` example).

With `systems`, natslens run watches every listed system in one process. Each has its own database and archive, collector connection, event connections, check engine, Kubernetes watcher and archive export, and its log lines carry `system`. A system whose NATS cannot be reached, or whose credentials are rejected, skips only its own epochs: the other systems, the API and readiness go on, and its status is `not_connected` or `connection_closed`. Before any system starts, natslens checks every system: its data directory belongs to it and every file the command reads exists: the `nats` credentials and TLS files and the inventory file always, and the account files and the kubeconfig in natslens run with events on. Any problem refuses the start with one error that names every broken system. After that check, a system whose setup fails (an events connection, a kubeconfig that does not parse) is isolated: its status is `connection_closed` with the error, an error is logged, and its setup is tried again, waiting 2 seconds doubling to a minute, while the other systems go on. A system whose database fails while running (reading its known servers, recording its start) still stops natslens. Each system's database gets its share of `duckdb_memory_limit` (see [Several systems](#several-systems)) and `max(2, GOMAXPROCS / number of systems)` DuckDB threads, so that the databases do not compete for every core; with one system that is `GOMAXPROCS`, which in a container follows its CPU limit. With more systems than half the CPUs, the floor of 2 threads per database adds up to more threads than CPUs. Two systems in one Kubernetes cluster each run a watcher, so node changes are recorded in both databases.

## Database

Each system has its own database, `natslens.duckdb` in its data directory (`data_dir` for a file without `systems`). Next to it, `system.json` records the system's id: a directory marked for another id refuses the start, and a directory without the file, as an earlier natslens left it, is adopted. All databases open in parallel before the API serves, each with its own rebuild from its archive; one that fails to open fails the start. With `systems`, natslens warns when a database in the top-level `data_dir` is used by no system, since its data is no longer read. `natslens sql` and `natslens checks run` read one system's database: `-config FILE -system ID`, or `-data-dir DIR`.

Schema changes are numbered migrations in [internal/store/migrations/](internal/store/migrations/). Never edit a migration that has been applied anywhere; add a new one. The `migrations` table records each file's SHA-256, and `natslens` refuses to start when an applied file no longer matches. A rebuild from the archive matches columns by name, so a new NOT NULL column on an archived table needs a default, or older hours no longer load.

Tables follow one pattern. Identity tables hold one row per thing with the first and last epoch it was seen; configuration tables hold one row per server per run of consecutive epochs with the same configuration (a run ends when the server reports nothing for the asset); stats tables hold one row per server per epoch and always carry `epoch` and `server_id`.

| Kind          | Tables                                                                                                                                                                   |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Epochs        | `epochs`, `epoch_endpoints`                                                                                                                                              |
| Identity      | `servers`, `accounts`, `streams`, `consumers`, `raft_groups`                                                                                                             |
| Configuration | `stream_config`, `consumer_config`                                                                                                                                       |
| Stats         | `server_stats`, `routes`, `gateways`, `leafnodes`, `ipqueues`, `health`, `connections`, `account_stats`, `account_details`, `stream_stats`, `stream_replicas`, `consumer_stats`, `consumer_replicas`, `raft_stats` |
| Events        | `events`                                                                                                                                                                 |
| Checks        | `findings`, `finding_transitions`, `check_runs`                                                                                                                          |
| Archive       | `archive_hours`                                                                                                                                                          |
| Operators     | `mutes`, `inventory`                                                                                                                                                     |

Every event has a `kind`: `client.connect`, `client.disconnect`, `client.auth_error`, `account.connections` (only with `events.account_connections`), `server.lameduck`, `server.shutdown`, `server.out_of_storage`, `server.removed`, `stream.created`, `stream.updated`, `stream.deleted`, `stream.leader_elected`, `stream.quorum_lost`, `consumer.created`, `consumer.deleted`, `consumer.leader_elected`, `consumer.quorum_lost`, `consumer.max_deliveries`, `consumer.nak`, `consumer.terminated`, `consumer.paused`, `consumer.resumed`, `consumer.ack_sampled`, `meta.leader_elected`, `jetstream.api_limit_reached`, `api.audit` (only with `events.api_audit`), `collector.gap`, `collector.started`, `collector.disconnected`, `collector.reconnected`, `collector.not_connected`, `collector.connection_closed`, `collector.subscription_error`, `collector.messages_dropped` and `collector.database_recovered`, among others. `raw` holds the payload with user JWTs removed.

`server_stats` also holds the VARZ connection limits: `max_payload`, `max_pending`, `write_deadline_ns`, `ping_interval_ns` and `max_pings_out` (migration 0025). They are NULL without a VARZ reply, and in rows rebuilt from hours archived before the migration.

`account_details` holds one row per ACCOUNTZ lookup of an account: the name, the system flag, imports and exports, and from the account JWT its issue time, expiry and limits (-1 for no limit, NULL without a JWT). Each account is looked up at most every 10 minutes, so read the newest row at or before an epoch. `server_stats` holds each listener certificate's expiry from VARZ (`tls_cert_not_after`, `cluster_tls_cert_not_after` and the gateway, leaf node, websocket and MQTT ones), and `connections` whether the client presented a user JWT (`user_jwt`) and when it expires (`jwt_expires`), all from migration 0028 and NULL in rows from before it. No JWT is stored.

A server gets a new `server_id` on every restart; join on `servers` for the stable `server_name`. To compare configuration across servers at epoch E, read only the rows current at E (`first_epoch <= E AND last_epoch >= E`); rows of a restarted server end at its last report. Read the column comments with:

```sql
SELECT table_name, column_name, comment FROM duckdb_columns() WHERE NOT internal ORDER BY table_name;
```

## Checks

A check is a file `checks/<code>.sql`: a YAML header in a block comment (code, name, entity, category, severity and when a finding gets another one, kind, lookback, thresholds with defaults and units, description, remediation, related configuration, for a check that returns a number `value_name` and `value_basis`, what it measures and how, and `suppressed_by`, checks of the same entity whose finding for an entity makes this one's redundant) and one SELECT. The query returns one row per failing entity with `entity_key`, `detail` and optionally `severity` (else the header's), `value`, `cause_event_id`, `related_config` (a JSON object of setting to value), `related_source` and `condition_since` (when the condition began, where the check knows it). It reads its inputs with `getvariable()`: `epoch`, `epoch_time` (the epoch's start), `prev_epoch`, and for a check with a lookback `lookback`, `lookback_text`, `window_start` and `window_epoch`, plus its own thresholds (a duration threshold is an INTERVAL). The macro `ns_text(ns)` writes nanoseconds as `7d`, `30s` or `1.5ms`. Three table macros describe servers that cannot be taken at their word: `restarting(lo, hi)` lists the answering servers whose process replaced an earlier one within `restart_window` (a JetStream error or raft groups catching up only add to their reasons, since a server in trouble shows the same); `planned_down(e)` the missing servers that announced lame duck or shutdown; `not_ready(e)` every server that may hold an asset without reporting it. Checks that judge the cluster (quorum, agreement, missing assets) leave restarting servers' reports out while the other servers can still form a majority, and never a report that is itself evidence of a quorum loss, so a rolling restart raises nothing above info except replicas offline or behind while they are (`TestRollingRestartRaisesNothing`). The headers are validated when natslens starts: a query that reads an undeclared variable or never reads a declared threshold is refused.

After every stored epoch, outside the epoch's transaction, the engine runs every check on one connection, each within `checks.timeout`, all within half an interval. It then writes, in one transaction, a `check_runs` row per check (`ok`, `error` or `disabled`) and the changes to `findings` and `finding_transitions`: a new failing entity starts `firing`, a severity change is `changed`, an entity no longer returned is `resolved`. A check that fails to run leaves its findings as they were and raises `system-001`, critical once the same check has failed ten epochs in a row; the order of the checks rotates by one each epoch, so a budget overrun does not leave out the same checks every time. A disabled check resolves its findings; a finding that resolved more than three times in the hour is flapping. Mutes are not applied when findings are written; they apply when findings are read. Transitions and check runs are swept and archived with their epoch; the archive holds the whole `findings` table in every hour, and a rebuild keeps each finding's newest copy.

Override a threshold, or the lookback of a check that has one, in the configuration file; an unknown code, threshold or value stops the start:

```yaml
checks:
  disabled: [stream-005]
  thresholds:
    consumer-006: {max_idle: 10m}
    consumer-002: {lookback: 1h, min_growth: 500}
```

The remediations describe the general case. To add your own advice (a runbook link, the team that owns a stream, how your deployment declares streams), point `checks.remediation_file` at a YAML file that maps a check code to text; the API and the UI show that text after the check's own remediation, below a `---` line. The text is plain, with `backticks` around code as in the checks' own remediations. The file is read at start only, so restart natslens after a change (in Kubernetes, a changed ConfigMap needs a rollout), and an unknown code stops the start:

```yaml
consumer-010: >-
  Runbook: https://runbooks.example.com/nats/missing-durable. The order team owns
  the order-jobs consumers.
stream-011: Streams are declared in the platform repository; recreate them from there.
```

To try a change to a check on a copy of a database: `natslens checks run -data-dir DIR [-epoch N]`. To read the findings: `natslens sql -data-dir DIR "SELECT id, severity, state, since, detail FROM findings WHERE state = 'firing'"`.

### Inventory

`inventory_file` lists per account the streams (with their durable consumers), KV buckets and object stores that must exist, and the settings that must have a given value; a setting left out is not compared. [inventory.example.yaml](inventory.example.yaml) describes an online shop that processes product media; [scripts/inventory.dev.yaml](scripts/inventory.dev.yaml) matches what `make load` creates, and `make churn` then shows consumer-010 naming the `load-churn` connection. The file is loaded into the `inventory` table on every start; without one the table is emptied.

### The checks

| Code | Name | Severity | Reads |
| ---- | ---- | -------- | ----- |
| server-001 | Server not reporting | warning; info for `restart_window` after it announced lame duck or shutdown; critical with half the servers missing, counting the streams and consumers that lost quorum | `epochs.missing_names`, `planned_down` |
| server-002 | Healthz not ok | critical | `health`; JetStream-only errors are server-003's, a restarting server's catch-up errors are left out |
| server-003 | JetStream not ready | critical, warning on a restarted server; JetStream disabled critical at once | JETSTREAM errors in consecutive epochs for `min_duration` (3m) |
| server-004 | Server restarted | info; critical above `max_restarts` unplanned restarts in 1h | `servers` processes per name; says whether it was an upgrade or a configuration change |
| server-005 | Version differs from the majority | info | `server_stats.version` |
| server-006 | Configuration reloaded | info | `config_load_time` after `start_time`, within 1h |
| server-007 | JetStream store usage high | warning 75%, critical 90% | `js_storage` and `js_memory` against their limits |
| server-008 | Memory far above the other servers | warning | `mem_bytes` against the other servers' median |
| server-009 | Slow consumers | warning; critical when a route, gateway or leaf node was a slow consumer | slow consumer counters, 15m, per process |
| server-010 | Stalled clients | warning | `stalled_clients`, 15m, per process |
| server-011 | Connection churn | warning | connections accepted within 10m at least half the open ones and at least 50, in 2 epochs in a row; a firing finding resolves below 0.8 of both; the detail names the clients that connected most |
| server-012 | Raft groups catching up for too long | warning | `raft_stats.catching_up` run longer than 5m |
| server-013 | Raft write error | critical | `raft_stats.wal_error` |
| server-014 | JetStream store full soon | warning 14d, critical 3d | `js_storage` growth over 6h per process, the smaller of the two halves' slopes |
| server-015 | Stale connections | info | `stale_connections`, 15m, per process |
| system-001 | Check failed to run | warning, critical after 10 epochs of the same failure | the engine's own runs |
| system-002 | No JetStream meta leader | critical | `meta_leader` of the clustered servers, or the leader seeing more peers offline than its quorum allows |
| system-003 | JetStream meta leader changing often | warning above 4 in 15m | `meta.leader_elected` events and the leader per epoch; one hand-over within two intervals after each step-down not counted |
| stream-001 | Stream quorum lost | critical | the leader most reporters name and its online replicas, or too few reporters when it does not report, 2 epochs; restarting servers left out only while the rest form a majority |
| stream-002 | Stream replica offline | warning; info while the replica's server restarts or announced its shutdown | the leader's `stream_replicas` |
| stream-003 | Stream replica lagging | warning | the leader's replica lag above 1000 operations |
| stream-004 | Stream near its message or byte limit | warning/critical; info for a limits stream with discard old | leader's messages and bytes against `max_msgs`, `max_bytes` |
| stream-005 | Stream without any limit | info | `stream_config`; a KV bucket or object store counts when it has neither a TTL nor max_bytes; left out: streams the inventory declares with max_age 0 and max_bytes -1, KV buckets the inventory lists that have allow_msg_ttl |
| stream-006 | Stream reaches its byte limit soon | warning/critical; limits streams with discard old left out | leader's bytes growth over 6h, the smaller of the two halves' slopes |
| stream-007 | Stream configuration differs across servers | warning | current `stream_config` rows, setting by setting, restarting servers left out |
| stream-008 | Stream creation time differs across servers | info; warning when a replica's last sequence or message count is behind the leader's by more than 100 or 1%, or ahead by more than 10 or 0.1% (first sequence: off by more than 100 or 1%), two epochs in a row | current `stream_config.created` and `stream_stats`, restarting servers left out |
| stream-009 | Stream peer set differs across servers | warning | the servers' peer sets, 2 epochs; restarting servers left out while the rest form a majority |
| stream-010 | Stream missing on an assigned server | warning, critical on most | leader's peers that answered JSZ without the stream and are not restarting |
| stream-011 | Expected stream or bucket missing | critical; info or warning while a server that held it is not ready | `inventory`, `stream_stats`, `not_ready`, `stream.deleted` events |
| stream-012 | Stream configuration differs from the inventory | warning | `inventory` against the leader's configuration |
| stream-013 | Stream configuration changed | info | `stream.updated` events with the client, 1h |
| stream-014 | Stream disappeared | critical with a delete event; info or warning while a server that held it is away | a stream reported within 1h (or since its server went away) that no server reports, not in the inventory |
| consumer-001 | Consumer quorum lost | critical | as stream-001 |
| consumer-002 | Consumer backlog growing | warning | leader's `num_pending` and delivered increases over 30m, growing in both halves |
| consumer-003 | Ack pending near max_ack_pending | warning 80% with messages waiting ("pool saturated" at the limit); critical when at the limit with no acknowledgement or delivery for ack_wait | leader's `num_ack_pending`, `num_pending`, ack floor and delivered sequence |
| consumer-004 | Many redeliveries | warning | `num_redelivered` at a quarter of `max_ack_pending`, at least 10 or `max_ack_pending` if smaller |
| consumer-005 | Messages reached max_deliver | warning | `consumer.max_deliveries` events, 1h |
| consumer-006 | No delivery while messages wait | warning | how long the backlog has waited without a delivery, 5m |
| consumer-007 | Push consumer without a subscriber | warning | leader's `push_bound` of durable push consumers, 2m |
| consumer-008 | Consumer configuration differs across servers | warning | as stream-007 |
| consumer-009 | Consumer creation time differs across servers | info; warning when a replica's delivered or ack floor position is behind the leader's by more than 100 or 1%, or ahead by more than 10 or 0.1%, two epochs in a row | current `consumer_config.created` and `consumer_stats`, restarting servers left out |
| consumer-010 | Expected consumer missing | critical; info or warning while a server that held it is not ready | `inventory`, `consumer_stats`, `not_ready`, `consumer.deleted` and `stream.deleted` events |
| consumer-011 | Consumer configuration differs from the inventory | warning | `inventory` against the leader's configuration |
| consumer-012 | Consumer missing on an assigned server | warning | leader's peers that answered JSZ without the consumer and are not restarting |
| consumer-013 | Durable consumer disappeared | critical with a consumer.deleted event; warning after its stream's delete; info or warning while a server that held it is away | a durable reported within 1h (or since its server went away) that no server reports while its stream is there; not internal, ephemeral, expiring or in the inventory |
| client-001 | High client round trip time | warning | `connections.rtt_ns` above 100ms on one client (server, pod and address, else cid) in 2 epochs in a row, by service (the connection name before "@") |
| client-002 | High pending bytes | warning | `connections.pending_bytes` above 4 MiB, by connection name |
| client-003 | Slow consumer disconnects | warning | `client.disconnect` events with a slow consumer reason, 30m |
| client-004 | Client reconnecting repeatedly | warning | 3 or more reconnects of one name in 10m: the smaller of the `client.disconnect` events not asked for by the client nor caused by a server stopping, and the `client.connect` events, of that name; instances sharing a name are merged |
| client-005 | Short-lived connections | warning | 20 or more connects of one name in 10m folded into `client.churn` events (closed by the client within `events.churn.pair_within`); instances sharing a name are merged; a firing finding resolves below 0.8 of the threshold |

To add a check, write its file, then a test in `internal/checks` that builds the rows it reads with the fixture helpers and shows one entity that fails and one that does not.

## Metrics

`natslens run` serves Prometheus metrics at `/metrics` on the API listener, so that the findings reach the alerting system. On a loopback listener the Host allowlist applies to `/metrics` as to the API. On any other listener `/metrics` is exempt from it, because Prometheus scrapes by the pod's address: the endpoint is read only but names streams, consumers, servers and clients, so such a listener must not be reachable from outside the cluster.

Every sample carries a `system` label, the ID of the system it describes (`default` until the configuration lists systems), and each family's `# HELP` and `# TYPE` appear once with the samples of every system below them. Each system is read on its own, in parallel and within `metrics_timeout` (8 seconds by default; keep it below the Prometheus `scrape_timeout`, usually 10 seconds, or Prometheus gives up on the whole target), and never twice at once: a scrape that finds the system's previous read still running counts it as failed at once. A system that fails, times out or panics leaves none of its series in the scrape, and the scrape still answers 200 for the others. Only when every system fails does `/metrics` answer 500, so with a single system a failure is a 500 and no `nats_lens_system_scrape_success 0` line appears. The label is new: a series scraped before it is a different series to Prometheus, so a dashboard or rule that joins series must add `system` to its `on(...)` and `by(...)` lists, and a series history starts again at the upgrade.

Every firing finding has three series with one identity, `system`, `code`, `entity_type` and `entity_key`, so that a change of severity does not start a new series. A mute does start a new `nats_lens_finding` series, because `muted` is its label; `nats_lens_finding_severity` and `nats_lens_finding_flapping` keep their identity:

| Metric | Labels | Meaning |
| ------ | ------ | ------- |
| `nats_lens_finding` | identity, `muted` (`true` or `false`) | 1 for each firing finding, muted or not; at most 50 unmuted entities per check, the worst first and then by entity key, so the set is stable; muted findings are always exported |
| `nats_lens_finding_severity` | identity | Severity of the finding: 1 info, 2 warning, 3 critical |
| `nats_lens_finding_flapping` | identity | 1 while the finding flaps |
| `nats_lens_finding_series_omitted` | `code` | Findings of a check left out of the three series by the cap |
| `nats_lens_findings_by_check` | `code` | Firing, unmuted findings of a check, every entity counted |
| `nats_lens_findings_by_check_max_severity` | `code` | Highest severity among them |
| `nats_lens_findings_total` | `severity` | Firing, unmuted findings by severity |
| `nats_lens_findings_muted` | | Firing findings a mute covers |
| `nats_lens_last_epoch_age_seconds` | | Age of the newest epoch; absent before the first |
| `nats_lens_last_epoch_partial` | | 1 when the newest epoch is partial |
| `nats_lens_epochs_last_hour`, `nats_lens_partial_epochs_last_hour` | | Epochs, and partial epochs, stored in the last hour |
| `nats_lens_epochs_without_check_runs_last_hour` | | Epochs of the last hour (the newest interval left out) for which no check ran |
| `nats_lens_event_messages_dropped_last_hour` | | Event messages the collector dropped in the last hour |
| `nats_lens_check_run_errors_last_hour` | | Check runs that failed in the last hour |
| `nats_lens_check_failing` | `code` | 1 for a check whose newest run failed |
| `nats_lens_system_scrape_success` | | 1 when the system's metrics were read for this scrape, 0 when reading them failed, took longer than `metrics_timeout` or found the previous read still running; then the system's other series are absent from that scrape |

On GKE, Cloud Monitoring alert policies can evaluate PromQL conditions over the data Managed Service for Prometheus (GMP) collects; the `PodMonitoring` in `k8s/components/gke-iap` scrapes `/metrics`. GMP adds the target labels `project_id`, `location`, `cluster`, `namespace`, `job` and `instance` to every series, so a query can select one deployment with `{cluster="...", namespace="nats-lens"}`. A metric that has a label of one of those names keeps it as `exported_<name>` (`exported_namespace` for a `namespace` label); the natslens metrics have none, so `namespace` is the pod's. An alert policy in Terraform or OpenTofu (one condition shown; the rules below translate the same way):

```hcl
resource "google_monitoring_alert_policy" "nats_lens_critical_finding" {
  project      = var.project
  display_name = "nats-lens critical finding (${var.environment})"
  combiner     = "OR"
  severity     = "CRITICAL"

  conditions {
    display_name = "A critical, unmuted nats-lens finding"
    condition_prometheus_query_language {
      query               = "nats_lens_finding_severity{namespace=\"nats-lens\"} >= 3 unless on(system, code, entity_type, entity_key) nats_lens_finding{namespace=\"nats-lens\", muted=\"true\"}"
      duration            = "120s"
      evaluation_interval = "60s"
    }
  }

  notification_channels = [var.notification_channel]

  documentation {
    subject   = "nats-lens finding on ${var.project}"
    content   = "Open /findings in nats-lens; mute the finding there if it is expected."
    mime_type = "text/markdown"
  }
}
```

The same rules in Prometheus form, for a Prometheus with Alertmanager:

```yaml
groups:
  - name: nats-lens
    rules:
      - alert: NatsLensCriticalFinding
        expr: nats_lens_finding_severity >= 3 unless on(system, code, entity_type, entity_key) nats_lens_finding{muted="true"}
        for: 2m
        labels: {severity: critical}
        annotations:
          summary: "nats-lens {{ $labels.code }} on {{ $labels.entity_key }} in {{ $labels.system }}"
          description: "Open /findings in nats-lens; mute it there if it is expected."
      - alert: NatsLensWarningFinding
        expr: nats_lens_finding_severity == 2 unless on(system, code, entity_type, entity_key) nats_lens_finding{muted="true"}
        for: 15m
        labels: {severity: warning}
      - alert: NatsLensNotCollecting
        expr: nats_lens_last_epoch_age_seconds > 180 or absent(nats_lens_last_epoch_age_seconds)
        for: 5m
        labels: {severity: critical}
        annotations:
          summary: "nats-lens has stored no epoch for 3 minutes; findings are stale"
      - alert: NatsLensChecksFrozen
        expr: nats_lens_epochs_without_check_runs_last_hour > 2
        for: 5m
        labels: {severity: critical}
        annotations:
          summary: "nats-lens stores epochs of {{ $labels.system }} but runs no checks; findings are stale"
      - alert: NatsLensCheckFailing
        expr: nats_lens_check_failing == 1
        for: 10m
        labels: {severity: warning}
      - alert: NatsLensSystemMetricsFailing
        expr: nats_lens_system_scrape_success == 0
        for: 5m
        labels: {severity: critical}
        annotations:
          summary: "nats-lens cannot read its data of {{ $labels.system }}; its findings are missing from /metrics"
```

A mute in the UI makes the finding rules stop matching, so the alert closes; the finding's series stay, marked `muted="true"`. Beyond 50 entities of one check, alert on `nats_lens_findings_by_check_max_severity` instead.

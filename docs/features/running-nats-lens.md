# Running nats-lens

- **Surfaces:** `api`, `cli`
- **Status:** shipped
- **Related:** [offline-datasets.md](offline-datasets.md), [monitoring-snapshots.md](monitoring-snapshots.md), [history-and-archive.md](history-and-archive.md), [metrics-and-alerting.md](metrics-and-alerting.md), [notifications.md](notifications.md), [kubernetes-context.md](kubernetes-context.md), [demo-mode.md](demo-mode.md), [design.md](../design.md)

## What it does

nats-lens is one binary, `natslens`, with the web UI built in. `natslens run` collects, captures events, runs the checks, archives the history and serves the API and the UI until it is stopped, for every NATS system the configuration lists ([multiple-systems.md](multiple-systems.md)). `natslens once` takes a single collection and exits. Settings come from defaults, a YAML configuration file and flags. The repository's `k8s/` directory holds Kubernetes manifests that run it as one replica beside a NATS cluster, with health and readiness endpoints for the probes.

## Behavior and rules

- **Repository verification.** Contributors can select fast or full verification and receive the first failing check with saved logs. Checks preserve local generated-source edits. The fast profile excludes heavy Go integration; the full profile includes it. See the [verification workflow and limits](../operations/verification.md).

- **Build artifacts.** `make build` verifies frontend inputs and output bytes, rebuilding assets only when necessary, then embeds them while holding the asset lock. `make build-full` forces generation. `make build-go` skips freshness checks and embeds existing assets, or the placeholder when assets are absent. Dependency/configuration/generated changes and missing outputs invalidate reuse. See [build guarantees and container-validation limits](../operations/build-reuse.md).

- **Remote investigation.** `natslens investigate` uses configured API profiles without opening a database or connecting to NATS. Its separate read-only route needs the existing authenticated access perimeter and a restricted proxy policy for least privilege ([agent-investigations.md](agent-investigations.md)).
- **Commands.** `natslens run` (also `natslens` alone) runs until SIGTERM or SIGINT. `natslens once` stores one collection of every system, or of the one `-system ID` names, runs the checks on it and exits, without events, archive or API. `natslens sql`, `natslens checks` and `natslens demo` are described in [sql-access.md](sql-access.md), [checks.md](checks.md) and [demo-mode.md](demo-mode.md).
- **Published builds.** Each release publishes the container image `ghcr.io/shadone/nats-lens` for linux/amd64 and linux/arm64 and binary archives for Linux and macOS on amd64 and arm64, with `checksums.txt` and build provenance attestations. The Linux binaries need glibc 2.34 and libstdc++ from GCC 12 or later; the macOS binaries need macOS 13 or later. See [releasing.md](../operations/releasing.md).
- **Version.** `natslens version` prints the version the binary was built with, the Go release and the platform, for example `natslens v0.1.0 (go1.27.2 linux/amd64)`, without reading a configuration. Release binaries and images carry their release tag; a binary built from a checkout prints `dev`. The same version appears in the start log line.
- **Test notifications.** `natslens notify test` reads the configured channels and sends test messages without opening a database or connecting to NATS ([notifications.md](notifications.md)). Only `run` schedules finding delivery; `once`, `checks run` and `demo` send no finding notifications.
- **Receiver access.** Notification delivery needs DNS and outbound HTTPS to configured receivers. A NetworkPolicy that restricts egress must allow those destinations, including when copying the GKE component.
- **Where settings come from.** Defaults first, then the file named by `-config`, then flags. The project's `natslens.example.yaml` lists every key. nats-lens checks the whole configuration at start and stops with every error it found, for example "timeout must be less than a third of interval: an epoch can take three collect windows".
- **Credentials stay out of the process list.** The system account password has no flag. nats-lens reads it from the file, or from `NATSLENS_NATS_PASSWORD` when `nats.user` is set and the file has none. An account with a `user` and no password in the file reads it from `NATSLENS_ACCOUNT_<NAME>_PASSWORD`, with the account name upper-cased and every character other than A-Z and 0-9 replaced by `_`. A credentials file (`nats.creds`) or TLS client certificates work instead. nats-lens never logs credentials. A password written into `-nats-url` does show in the process list.
- **Logs.** nats-lens writes JSON logs to standard error, at `log_level` (`info` by default). The start line lists the settings, among them `connz_limit`, `subscriptions_interval` and `subsz_limit`.
- **How much is collected.** `connz_limit` (4096) caps the connections stored per server per collection. `subscriptions_interval` (5 minutes) is the shortest time between two collections that read the servers' subscription lists, and `0` turns them off; `subsz_limit` (10,000, at most 100,000) caps the subscriptions stored per server per list. Every server's list arrives on the collector's one connection within one window, and the server it is connected to closes that connection as a slow consumer once the replies waiting for it pass its `max_pending` (64 MB by default); keep `subsz_limit` × servers × about 250 bytes well under it, or raise `max_pending` on the servers. See [monitoring-snapshots.md](monitoring-snapshots.md); the [Subjects](subjects.md) page shows the lists, and says when one was cut or not collected.
- **Micro services, per account.** Each account under `accounts` is asked for its micro services (`$SRV.INFO` and `$SRV.STATS`) over its connection; its user needs publish on `$SRV.INFO` and `$SRV.STATS` (`$SRV.>` is enough too) and subscribe on `_INBOX.>`. `micro: false` on an account turns the asking off for that account. A user without the grant does not stop collection: each collection records the refusal for that account, with the server's text, and nats-lens logs it once as a warning, again when it changes and once when it ends. A refused request is not sent again for 10 minutes, as the NATS servers log each refusal as an error, so a grant added later takes effect within 10 minutes. On an upgrade, the accounts already under `accounts` are asked by default; without the grants the server the entry's connection is on logs each refusal once per 10 minutes per entry and per request (two entries for one account log twice; INFO and STATS each when both are refused), until `micro: false` is set on the entry or the grants are added. A dialled-again connection, for example with new credentials, is asked at once. Two entries for one account ask it once and store its answers once. In `natslens run`, with or without event capture, an account that cannot connect (for example a missing `creds` file) does not stop the system: it is logged and dialled again while the system collects, with one warning per dial: after the backoff reaches a minute, about 60 warnings an hour. A missing or unreadable account file does not refuse the start: nats-lens logs a warning at start that names the system, the entry and the file, for every entry under `accounts`, whether or not the account is dialled. nats-lens opens one connection per account, with or without event capture. The Micro services page shows the answers, and says on the page when an account refused ([micro-services.md](micro-services.md)).
- **The API and UI address.** `listen` is `127.0.0.1:8080` by default; an empty value turns the API and UI off. On a loopback address the API accepts only the Host names localhost, 127.0.0.1 and ::1, against DNS rebinding from a web page; `allowed_hosts` changes the list.
- **No exposure without an identity proxy.** nats-lens has no sign-in of its own. It refuses to listen on a non-loopback address unless `iap_audience` is set, or `allow_unauthenticated: true` says that something else (for example a NetworkPolicy) keeps strangers out. With `allow_unauthenticated` it logs a warning that anyone who reaches the API can mute findings.
- **Behind IAP.** With `iap_audience` set to the backend service's audience, mutes, lifts, acknowledgements, and hand-backs need a valid signed IAP assertion, and nats-lens records its user. Reads never need one.
- **Safe responses.** Every response carries a Content-Security-Policy that allows the page's own files, its inline theme script and styles by their SHA-256 hashes, and `data:` images, and forbids framing; plus `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer`. The UI makes no third-party requests and hosts its own fonts.
- **Health and readiness.** `/healthz` answers while the process serves. `/readyz` answers ok once every system's database is open (any rebuild from the archive has finished) and the API serves. Readiness does not follow collection, so the UI stays reachable while NATS is down; a collector that stopped shows in the UI header and in the `nats_lens_last_epoch_age_seconds` metric. During a rebuild at start, neither endpoint answers yet. A typed fatal database error makes `/readyz` answer 503; `/healthz` and the API keep serving.
- **Fatal database errors.** nats-lens stops database work and collection for the affected system. Logs and system summaries show a bounded diagnostic with the operation stage; database row dumps and arbitrary driver text are omitted. Separate healthy systems continue collecting. Calls already running are canceled where possible and unwind. nats-lens does not reopen or repair the failed database. See [multiple-systems.md](multiple-systems.md).
- **Stopping.** On SIGTERM or SIGINT, a running collection write may finish for up to `shutdown_grace` (5 seconds), an in-flight notification request gets the same `shutdown_grace` before cancellation, leaving its batch pending; the last events are written until `shutdown_grace` plus `events.flush_interval`, and the API gives calls in progress 5 seconds. Several systems stop at the same time within the same bounds. The process manager's stop timeout must be longer than `shutdown_grace + events.flush_interval + 2s` or 5 seconds, whichever is longer, plus a second or two to exit; with the defaults, at least 10 seconds.
- **Memory.** DuckDB may use `duckdb_memory_limit`, by default 60% of the container's memory limit, or 1 GiB outside a container. Several systems share it, each with its own database.
- **The Kubernetes manifests.** `k8s/base` creates the namespace `nats-lens`, a service account with read-only roles, a volume, the Deployment and the Service, and expects two Secrets with the NATS credentials. `k8s/components/gke-iap` adds IAP, a load balancer health check on `/healthz`, a NetworkPolicy and a Managed Prometheus scrape of `/metrics`. Two overlays to copy: `k8s/examples/basic`, reached by `kubectl port-forward` only, and `k8s/examples/gke-iap`, behind IAP on an existing Gateway.
- **One replica.** DuckDB has one writer, so the Deployment runs one replica with the `Recreate` strategy, and the database and the archive share a volume. The pod prefers a node without a NATS server, so draining a NATS node does not also take nats-lens away. It runs as non-root on a read-only root filesystem.
- **Out-of-the-box limits.** The base asks for 100m CPU and 512 MiB of memory with a 5 GiB volume (a laptop cluster or a small system); the `gke-iap` example asks for 500m, 2 GiB and 20 GiB as a starting point for production. Reading the Topology of a system with 1,000 leaf node sites takes up to about 450 MB in the process, more when servers' LEAFZ answers failed during the day, so a system with hundreds of leaf node sites is estimated to need more, and several systems in one process need more. The [Memory size](../deploy.md#memory-size) table in the deploy docs gives a memory limit per size of system; its figures are proposed, not yet confirmed.

- **Offline acquisition.** The [offline dataset channel](offline-datasets.md) creates a full-system artifact at one database read boundary. Hourly recovery archives retain their own publication boundaries and are not exact-time investigation snapshots.

## Scenarios

### Run against a NATS system

- **Surfaces:** `cli`
- **Given** a system account user with the grants of [monitoring-snapshots.md](monitoring-snapshots.md)
- **When** I run `NATSLENS_NATS_PASSWORD=... natslens run -nats-url nats://nats.example.com:4222 -nats-user natslens`
- **Then** nats-lens collects every 30 seconds into `./data` and serves the UI at http://127.0.0.1:8080

### Print the version

- **Surfaces:** `cli`
- **Given** a binary from the `v0.1.0` release for linux/amd64
- **When** I run `natslens version`
- **Then** it prints `natslens v0.1.0 (go1.27.2 linux/amd64)` and exits, with the Go release it was built with

### Refuse to serve openly by mistake

- **Surfaces:** `cli`
- **Given** a configuration with `listen: 0.0.0.0:8080` and no `iap_audience`
- **When** nats-lens starts
- **Then** it stops with an error saying that the listen address is not loopback and `iap_audience` is empty

### Probe a pod

- **Surfaces:** `api`
- **Given** nats-lens runs in Kubernetes from the base manifests
- **When** the kubelet calls `/healthz` and `/readyz`
- **Then** both answer ok once the database is open and the API serves, also while NATS is unreachable

### Contain a fatal database error

- **Surfaces:** `api`, `cli`
- **Given** nats-lens serves a system whose database encounters a typed fatal error
- **When** the engine reports that error during a database operation
- **Then** further operations on that engine are refused and that system's workers stop
- **And** `/readyz` answers 503, `/healthz` keeps answering, and the error omits database row dumps

### Deploy for port-forward access

- **Surfaces:** `cli`
- **Given** a cluster with NATS from the Helm chart in namespace `nats`, and a CNI that enforces NetworkPolicy
- **When** I create the Secrets `natslens-nats-system` and `natslens-nats-app`, set the image, and run `kubectl apply -k k8s/examples/basic`
- **Then** nats-lens runs in namespace `nats-lens`, and `kubectl -n nats-lens port-forward svc/natslens 8080` opens the UI at http://127.0.0.1:8080

### Size the pod's memory

- **Surfaces:** `cli`
- **Given** one system deployed from `k8s/examples/basic`, whose pod asks for 512 MiB with limits equal to requests
- **When** nats-lens starts
- **Then** its log line `duckdb memory limit` shows `"bytes":322122547`, 60% of 512 MiB

### Allow a notification receiver

- **Surfaces:** `cli`
- **Given** a channel destination is configured and a NetworkPolicy limits the pod's egress
- **When** the policy allows DNS and HTTPS to that receiver and I run `natslens notify test`
- **Then** the command reports whether the receiver accepts the test without requiring a stopped collector

### Verify a change without rewriting local work

- **Surfaces:** `cli`
- **Given** a prepared checkout with installed verification tools and a local generated-source edit
- **When** I run the selected verification profile
- **Then** drift fails the check without overwriting the edit, later checks stop, and the logs identify the failing stage
- **And** a selected subset is labeled as incomplete verification

## Not supported / out of scope

- **No automatic recovery from a typed fatal database error.** Containment does not establish a cause or repair stored data.
- **No high availability.** One replica at a time; a second would not get the database.
- **No authentication of its own.** Use an identity-aware proxy, or keep the API on loopback or behind a NetworkPolicy.
- **No TLS on the API listener.** The API speaks plain HTTP (and HTTP/2 without TLS for gRPC); TLS ends at the proxy or load balancer.
- **No configuration reload.** nats-lens reads its configuration, inventory and remediation file at start; a change needs a restart. Notification secret files are read at each delivery attempt, so rotating their contents needs no restart.
- **The `basic` overlay needs an enforcing CNI.** Without NetworkPolicy enforcement any pod can reach the API and mute findings.
- **No Helm chart.** The manifests are kustomize only.

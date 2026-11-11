# Configuration

This page lists every configuration key and flag, how several NATS systems share one natslens, and the NATS permissions natslens needs. For a deployment, read [Deploy](deploy.md) as well.

Configuration comes from defaults, then an optional YAML file (`-config`), then flags. [natslens.example.yaml](../natslens.example.yaml) lists every key.

| Key                   | Flag             | Default                 | Description                                              |
| --------------------- | ---------------- | ----------------------- | -------------------------------------------------------- |
| `interval`            | `-interval`      | `30s`                   | Time between collections.                                |
| `timeout`             | `-timeout`       | `3s`                    | Collect window for one request; under a third of `interval`. |
| `data_dir`            | `-data-dir`      | `./data`                | Directory for `natslens.duckdb`.                         |
| `retention`           | `-retention`     | `14d`                   | Epochs older than this are deleted.                      |
| `server_forget_after` | `-server-forget-after` | `1h`              | How long a server that stopped answering is expected; must be positive. |
| `connz_limit`         | `-connz-limit`   | `4096`                  | Most connections stored per server per collection.      |
| `subscriptions_interval` | `-subscriptions-interval` | `5m`         | Shortest time between two collections of the servers' subscription lists (SUBSZ), taken with a collection; `0` turns them off. |
| `subsz_limit`         | `-subsz-limit`   | `10000`                 | Most subscriptions stored per server per subscription collection; at most 100,000. |
| `shutdown_grace`      | `-shutdown-grace` | `5s`                   | How long a collection's write may continue after a stop signal; positive and at most `interval`, since a write never runs longer than one interval. |
| `log_level`           | `-log-level`     | `info`                  | `debug`, `info`, `warn` or `error`.                      |
| `system_id`           |                  | `default`               | Id of the one system a file without `systems` describes: lowercase letters, digits and `-`, at most 63, starting with a letter or digit, and not `all`, `unknown` or `other`, which `/metrics` gives timings that belong to no one system. Used in URLs, the API and metric labels. |
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
| `query_budget`        |                  | one per 3 DuckDB threads, at least 1 | Queries the API and `/metrics` may run at once on each system's database (4 on 12 threads, a share when systems divide the threads). Others wait in turn up to their call's deadline, then the call answers `resource_exhausted`; epoch writes, checks and other background work never wait for it. While an epoch write runs, new queries are admitted up to one. Proposed, not yet confirmed; see [Query budget](design.md#query-budget-proposed-not-yet-confirmed). |
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
| `accounts`            |                  |                         | Accounts whose advisories are captured and whose micro services are asked; see below. |
| `inventory_file`      | `-inventory-file` |                        | YAML file of expected streams, consumers and buckets ([inventory.example.yaml](../inventory.example.yaml)); read on every start. |
| `checks.timeout`      |                  | `5s`                    | Longest one check's query may run; at most `interval`. |
| `checks.disabled`     |                  |                         | Check codes that do not run; their findings resolve. |
| `checks.include_tooling` |              | `false`                 | Count tooling connections in the client checks (`client-001` to `client-009`) and in `server-011`'s client list: nats-lens's own connections (`nats.name`, `<name>-events`, `<name>-<account>`) and clients connected from 127.0.0.1 or ::1, such as a port-forwarded CLI. The API marks them `tooling` either way. |
| `checks.loopback_is_tooling` |          | `true`                  | Clients connected from 127.0.0.1 or ::1 are tooling. When such clients are more than 60% of the connections that are not nats-lens's own over the last 10 epochs (a sidecar that makes inbound traffic look local), they are not, until the share falls below 40%; natslens logs it once. An epoch with fewer than 3 such connections keeps the previous decision. |
| `checks.restart_window` |                | `10m`                   | How long a restarted server counts as restarting: checks that judge the cluster leave its own reports out, and a planned restart is reported at info. |
| `checks.thresholds`   |                  |                         | Threshold overrides: check code to threshold name to value; see [Override thresholds](checks.md#override-thresholds). |
| `checks.remediation_file` |             |                         | YAML file mapping check codes to extra text or `{text, playbook}` with an HTTPS playbook URL. The text appends to the check's own after a `---` line. Markdown guidance with inline code, links, lists and tables; raw HTML stays inert. Read at start only: restart natslens to apply a change (in Kubernetes, a changed ConfigMap needs a rollout). An unknown code stops the start. See [Add site-specific remediation](checks.md#add-site-specific-remediation). |
| `notifications.ui_url` | | none | Absolute HTTP or HTTPS UI base URL for message links; absent means no links. Shared across systems. |
| `notifications.channels` | | none | Shared list of webhook or Slack-compatible receivers; no channels means no direct delivery. |
| `notifications.channels[].name` | | required | Unique lowercase letters, digits and `-`, at most 63, starting with a letter or digit; identifies the channel in logs and metrics. |
| `notifications.channels[].type` | | required | `webhook` (versioned JSON) or `slack` (a `text` message). |
| `notifications.channels[].url`, `.url_file`, `.url_env` | | exactly one required | Destination URL, file holding it, or environment variable naming it. HTTPS, or HTTP on loopback only. Secret URLs belong in a file or variable. |
| `notifications.channels[].bearer_token_file`, `.bearer_token_env` | | none | At most one token source; webhook only. Files and variables are read at each delivery attempt. |
| `notifications.channels[].min_severity` | | `warning` | Lowest selected severity: `info`, `warning` or `critical`. |
| `notifications.channels[].systems`, `.checks`, `.categories` | | all | Lists of system IDs, check codes and categories; an empty list selects all. Unknown values stop startup. |
| `notifications.channels[].exclude_checks` | | none | Check codes never selected; overrides `checks`. |
| `notifications.channels[].kinds` | | all | `firing`, `changed`, `resolved`, `flapping`, `muted`, `reminder`, `acknowledged`. Empty selects all; baseline and test always send. |
| `notifications.channels[].repeat_every` | | `0` | Regular reminders disabled at `0`; otherwise at least `1h`. A quiet-ended reminder does not require this setting. |
| `notifications.channels[].timeout` | | `10s` | Positive request timeout, at most `60s`. |
| `notifications.channels[].give_up_after` | | `24h` | Positive maximum age of a still-failing batch before give-up. |
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
| `kubernetes.client_pod_watch.namespaces` |  | none                 | Optional. Namespaces whose pods are watched so that the Applications page shows where each application runs ("Runs on"). Needs `kubernetes.enabled` and a Role per namespace that may list and watch pods (`k8s/components/client-pods`). Unset, natslens asks for no client pod and needs no such permission. |
| `kubernetes.client_pod_watch.all_namespaces` | | `false`            | Watch the pods of every namespace instead, with one cluster-wide watch; needs a ClusterRole (`k8s/components/client-pods-all`). Not with `namespaces`. |

## Shutdown

On SIGTERM or SIGINT natslens lets a running epoch write finish for up to `shutdown_grace`. Notification delivery stops scheduling requests; an in-flight request gets until the same grace deadline, then cancellation leaves its batch pending for restart. At the same time it drains the event connections, all at once, for at most `events.flush_interval`, and writes the buffered events until `shutdown_grace` plus `events.flush_interval` after the signal; events that do not fit by then are counted in a `collector.messages_dropped` event. An archive export running at the signal is cancelled; natslens waits for it until the same deadline and abandons it there. The last `state.json` is written after the last events, by that deadline or, when the events used it up, within 2 seconds after it. With several systems, every system follows these steps at the same time against the one deadline. The API stops accepting connections at the signal and gives calls in progress up to 5 seconds from then before it closes their connections; the databases close only after the API stopped, all at once, by the same deadline as the last `state.json`. A database still closing then is left to the next start, which replays its write-ahead log. A signal while the databases open (a long rebuild) ends natslens cleanly, with exit status 0. Every other shutdown step ends by `shutdown_grace` plus `events.flush_interval` after the signal (the events' built-in 10-second final wait applies only when no deadline is set, which `natslens run` always sets). So the one rule is: the process manager's stop timeout (Kubernetes `terminationGracePeriodSeconds`, systemd `TimeoutStopSec`) must be longer than `shutdown_grace + events.flush_interval + 2s` or 5 seconds, whichever is longer, plus a second or two for the process to exit. With the defaults (5s and 1s) that is 8 seconds plus the margin, so at least 10 seconds; the Deployment's 30 seconds is enough.

## Passwords

The password has no flag, so that it never shows in the process list. A password written into `-nats-url` (`nats://user:pass@host`) does show there; put it in the file or `NATSLENS_NATS_PASSWORD` instead.

## Accounts

JetStream advisories are published inside the account that owns the asset, so each account needs its own connection. Every entry in `accounts` takes `name` (the account name the servers use, for example `APP`), `url` (defaults to `nats.url`), `creds` or `user` and `password`, and `tls` (the CA defaults to `nats.tls.ca`; the client certificate is never inherited). When an account has a `user` and no password in the file, the password is read from `NATSLENS_ACCOUNT_<NAME>_PASSWORD`, with the name upper-cased and other characters replaced by `_`. The account connection asks `$SYS.REQ.USER.INFO` for the account's name as the servers use it, and events from it carry that name. Until it is known (just after connecting), and for the whole run when the user may not request it or may not receive the reply, they carry the configured `name` instead, so filter on both when the two differ.

The same connection, one per account, asks the account's micro services: `$SRV.STATS` every epoch and `$SRV.INFO` on the first epoch, every `subscriptions_interval` and when STATS names an instance not yet described. `micro: false` on an entry turns this off for that account. Without event capture (`events.enabled: false`) `natslens run` still opens a connection for each account that asks. With or without event capture, an account that cannot connect (for example a missing `creds` file) is logged, dialled again with the backoff of a closed connection, and recorded as not connected meanwhile, while the other accounts and the epochs go on: its micro requests are recorded as `account connection not connected`, and with event capture a `collector.not_connected` event names it, until a `collector.reconnected` when it connects; its advisories are captured and its micro services asked from then on. A missing file of an `accounts` entry (its `creds` or a TLS file) refuses no start: the start check logs a warning naming the system, the entry, the file's key (such as `accounts[APP].creds` or `accounts[APP].tls.ca`) and its path, once per file that cannot be opened (missing or unreadable), for every entry, whether or not the command dials that account, with event capture on or off (the owner's decision of 2026-10-26). `natslens once` connects each asking account for its one epoch and records one that does not connect as not connected, without dialling again. Two entries whose users are in one account ask it once and store one set of answers per epoch, under the first entry whose STATS is not refused; an entry whose `$SYS.REQ.USER.INFO` lookup is refused counts as the account its `name` names, so in operator mode, where the account is a public key, it is never matched and its answers are stored beside the other entry's. A refused or unanswered request is recorded in `micro_requests` and fails nothing. On an upgrade, every account already listed under `accounts` is asked by default: without the `$SRV` grants below, the server the entry's connection is on logs each refusal once per 10 minutes per entry and per request (two entries for one account log twice; INFO and STATS each when both are refused), and `micro: false` on the entry stops it.

## Several systems

`systems` lists the NATS systems one natslens watches. Each entry takes `id` (as `system_id`, with the same reserved values; ids must differ), `name` (display name, default the id), `environment` (free text shown with the system) and its own `nats`, `accounts`, `inventory_file`, `kubernetes`, `retention`, `archive_retention`, `data_dir`, `archive_dir` and `duckdb_memory_limit`, with the same meaning and defaults as the top-level keys, except:

| System key | Default |
| --- | --- |
| `nats.name` | `natslens-<id>`, so each system's connections can be told apart in CONNZ and server logs; the tooling names follow it (`natslens-<id>-events`, `natslens-<id>-<account>`). |
| `retention`, `archive_retention` | The top-level value. |
| `data_dir` | `<data_dir>/<id>`: the top-level `data_dir` is the root. |
| `archive_dir` | `<archive_dir>/<id>` when the top-level `archive_dir` is set, else `<system data dir>/archive`. |
| `duckdb_memory_limit` | An equal share of what the systems' own limits leave of the top-level `duckdb_memory_limit` (or its default), at least 256 MiB. The systems' total can therefore exceed the budget: when their own limits alone add up to more, or when the 256 MiB floor applies (for example 4 systems without a limit and a 768 MiB budget). natslens logs a warning at start in both cases, naming the floor when it caused it. |

With `systems`, the top-level `nats`, `accounts`, `inventory_file`, `kubernetes` and `system_id` and the `-nats-*` and `-inventory-file` flags are refused, as is an empty `systems`. Two systems may not share directories: an archive directory may not be equal to or inside another system's archive or data directory, and a data directory may not be equal to another's or inside another system's archive directory (symbolic links are resolved component by component, so `..` after a link leaves the link's target; existing directories are compared as files, so on a case-insensitive file system `Data` and `data` match, but two spellings of a directory that does not exist yet are not recognised as one). A data directory inside another system's data directory is allowed. Every other key (interval, timeout, listen, IAP, events, checks, `connz_limit`, `subscriptions_interval`, `subsz_limit`, `shutdown_grace`) applies to all systems. A system password not in the file is read from `NATSLENS_SYSTEM_<ID>_NATS_PASSWORD` and an account password from `NATSLENS_SYSTEM_<ID>_ACCOUNT_<NAME>_PASSWORD`, with the id and name upper-cased and other characters replaced by `_`; Two connections that would read the same variable, because each has a `user` and no `password` in the file, (for example id `a` with account `b_account_c` and id `a-account-b` with account `c`, or accounts `a-b` and `a_b` of one system) are refused; this also holds for the accounts of a file without `systems`. `NATSLENS_NATS_PASSWORD` and `NATSLENS_ACCOUNT_<NAME>_PASSWORD` apply only to a file without `systems`; when they are set while `systems` is used, natslens logs one warning that names them and ignores them.

A file without `systems` is one system with the id `system_id` whose database and archive stay exactly where they are (`data_dir` and `archive_dir`), so adding `system_id` to an existing file moves nothing. natslens marks each system's data directory with its id on first start (see [Database](database.md)): to move an existing single-system file to `systems` and keep its data, list the system with the id it had (`default` unless `system_id` was set) and set its `data_dir` and `archive_dir` to the old directories.

## NATS permissions

natslens only subscribes and makes requests; it never publishes to an application subject. Use a scoped user in the system account rather than the account's full user: a system account user without permissions may also send the `$SYS.REQ` calls that change servers (for example `$SYS.REQ.SERVER.<id>.RELOAD` or kick and lame duck requests). The `$SYS.SERVER.>` subscription includes `$SYS.SERVER.*.OCSP.PEER.CONN.REJECT`; an explicit subject allowlist must include that subject too. No additional publish or account grants are needed. These are the minimal grants; `TestCollectWithScopedSysUser`, `TestCaptureWithScopedUsers` and `TestMicroScopedUser` show they are enough, and each test fails when one is taken out.

| User | Publish | Subscribe | Used for |
| ---- | ------- | --------- | -------- |
| System account (`nats.creds`) | `$SYS.REQ.SERVER.PING.>`, `$SYS.REQ.ACCOUNT.PING.STATZ` | `$SYS.ACCOUNT.>`, `$SYS.SERVER.>`, `$JS.EVENT.ADVISORY.>`, `_INBOX.>` | Discovery and the detail requests of every epoch (replies arrive on `_INBOX.>`); connect, disconnect, auth error, OCSP peer rejection, lame duck and shutdown events; the advisories that concern no account |
| Each account in `accounts` | `$SYS.REQ.USER.INFO`, `$SRV.INFO`, `$SRV.STATS` | `$JS.EVENT.>`, `$SYS.ACCOUNT.CLIENT.AUTH.ERR`, `_INBOX.>` | Stream and consumer advisories and metrics; account-scoped auth errors; the account name lookup; the micro service requests; their replies |

In a NATS server configuration (with JWTs, set the same lists on the user with `nsc edit user --allow-pub ... --allow-sub ...`):

```
permissions: {
  publish:   { allow: ["$SYS.REQ.SERVER.PING.>", "$SYS.REQ.ACCOUNT.PING.STATZ"] }
  subscribe: { allow: ["$SYS.ACCOUNT.>", "$SYS.SERVER.>", "$JS.EVENT.ADVISORY.>", "_INBOX.>"] }
}
```

```
permissions: {
  publish:   { allow: ["$SYS.REQ.USER.INFO", "$SRV.INFO", "$SRV.STATS"] }
  subscribe: { allow: ["$JS.EVENT.>", "$SYS.ACCOUNT.CLIENT.AUTH.ERR", "_INBOX.>"] }
}
```

An account user refused `$SYS.REQ.USER.INFO`, or its `_INBOX.>` reply, keeps the configured label for the run and logs one Info line; nothing else fails. An account user refused `$SRV.INFO` or `$SRV.STATS` gets the refusal recorded in `micro_requests` (state `error`, with the server's text); the refused request is not sent again for 10 minutes, as nats-server logs every refused publish as an error; each epoch in between records it with state `held`, the refusal in `error`, and `refused_at` and `retry_after`. A grant added later takes effect within 10 minutes, or at once when the entry's connection is dialled again. The refusal is logged once as a warning, again when it changes and once at Info when it ends; it is no `collector.subscription_error` event, and nothing else fails. A refused `$SYS.REQ.USER.INFO` is asked again after 10 minutes for the micro services. An account with `micro: false` needs neither.


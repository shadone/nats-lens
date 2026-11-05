# Checks and findings

- **Surfaces:** `ui`, `api`, `cli`
- **Status:** partial — accepting a finding is built for one entity; related entities, a check everywhere and a review list are planned
- **Related:** [server-checks.md](server-checks.md), [account-checks.md](account-checks.md), [stream-checks.md](stream-checks.md), [consumer-checks.md](consumer-checks.md), [client-checks.md](client-checks.md), [system-checks.md](system-checks.md), [inventory.md](inventory.md), [triage-and-mutes.md](triage-and-mutes.md), [metrics-and-alerting.md](metrics-and-alerting.md), [notifications.md](notifications.md), [design.md](../design.md)

## What it does

After every collection, nats-lens runs its checks against what it just stored. Each check looks for one kind of problem, such as a stream that lost quorum, a consumer whose backlog keeps growing, or a server whose disk fills in three days. It reports every entity where it finds that problem. Each report is a **finding**: one check failing for one server, account, stream, consumer, client or for the whole system. A finding stays open while the problem lasts and resolves on its own when the problem goes away. nats-lens ships 82 checks. You can tune their thresholds, turn single checks off, add your own remediation text, and mute findings you have decided to live with, or accept one entity's finding as by design until a review date.

## Behavior and rules

- **One finding per check and entity.** A finding has a check code (for example `stream-001`), an entity, a severity, a one-line detail and, where the check measures something, a value with its name, such as "replicas online". Client findings are keyed by connection name (for `client-001`, the part of the name before "@"), so a finding survives reconnects.
- **Three severities.** `critical` means messages are at risk now, for example quorum is lost or a durable consumer disappeared. `warning` means something will break or is degrading. `info` means something worth knowing, such as a restart, a version difference or a stream without limits. A check has a default severity, and many checks raise or lower it by rule; for example, server store usage is a warning from 75% and critical from 90%.
- **Findings start, change and resolve by themselves.** A newly failing entity starts a finding as **firing**. A severity change is recorded as **changed**. When the check no longer reports the entity, the finding is **resolved**. Every step is kept as a transition with the time of its collection, and every transition is also an event.
- **Flapping is called out.** A finding that resolved more than three times in the hour before a collection is marked **flapping**, so it is not mistaken for a series of separate incidents.
- **Two start times.** "First seen by nats-lens" is when nats-lens first reported the finding. "Condition since" is when the problem itself began, where the check can tell: for example, the moment a server process started for `server-004`, or the newer creation time for `stream-008` and `consumer-009`. A finding first seen within a minute of the start of nats-lens's history says "first seen when nats-lens history begins", because the problem may be older.
- **Each check has a category.** The categories are health, agreement (servers disagree with each other or an asset vanished), inventory (what the inventory file declares is missing or different), consumers, storage, clients, change, expiry (a certificate or JWT that expires soon; its warnings rank after the other warnings, so they never head an outage), and engine (nats-lens's own checks failing).
- **Planned maintenance does not raise alarms.** A server is restarting when its process started within `checks.restart_window` (10 minutes by default) and replaced an earlier process of the same name. A missing server that announced lame duck mode or shutdown first is a planned stop. The checks that judge the cluster (quorum, agreement, missing assets) leave a restarting server's reports out while the other servers still form a majority. A rolling restart therefore raises only info findings, plus warnings for replicas while they are offline or behind.
- **Short blips need to persist.** Checks prone to one-off spikes need the condition in two collections in a row before they fire: stream and consumer quorum loss, stream peer set differences, replica positions that differ in `stream-008` and `consumer-009`, connection churn and client round trip time. Some checks need a duration instead, such as `server-003` (JetStream not ready for 3 minutes) and `server-012` (raft groups catching up for 5 minutes).
- **One problem, one finding.** Where two checks would say the same thing, one leaves it to the other: `stream-009` (peer sets differ) stays quiet for a stream while `stream-001` (quorum lost) fires for it, and `server-002` (healthz) leaves JetStream readiness errors to `server-003`.
- **Tooling connections are left out.** The client checks (`client-001` to `client-008`) and the client list in the detail of `server-011` ignore tooling connections, unless `checks.include_tooling` is set. Tooling connections are nats-lens's own connections (`nats.name`, `<name>-events`, `<name>-<account>`) and, with `checks.loopback_is_tooling` (on by default), clients connected from 127.0.0.1 or ::1, such as a port-forwarded `nats` CLI.
- **The loopback guard.** A sidecar proxy can make every inbound connection look local. When loopback clients are more than 60% of the connections that are not nats-lens's own over the last 10 collections, nats-lens counts them as real clients again, until their share falls below 40%. A collection with fewer than 3 such connections keeps the previous decision, so a lone CLI stays tooling. nats-lens logs it once when the guard turns the rule off.
- **Thresholds are settings.** Every number a check compares against is a named threshold with a default and a unit (count, ratio, bytes, duration, or version, a dotted version such as `1.37.0` for `client-008`'s minimum per client library). The configuration file can override a threshold under `checks.thresholds`, or the lookback of a check that has one. `checks.disabled` turns single checks off; their open findings then resolve. nats-lens refuses to start when the configuration names an unknown check, threshold or value.
- **Every finding says what to do.** Each check has a description of what it detects and a remediation: the steps to investigate or fix the problem. A finding also shows its related settings with their current values, the basis of its value, and the event that caused it, where the check names one.
- **Your own remediation text.** `checks.remediation_file` points at a YAML file that maps a check code to extra text, such as a runbook link or the team that owns a stream. The API and the UI show that text after the check's own remediation, below a `---` line. The text is plain, with `backticks` around code; nats-lens renders no other Markdown. nats-lens reads the file at start only, so a change needs a restart (in Kubernetes, a changed ConfigMap needs a rollout). An unknown check code or an empty text stops the start.
- **A failing check is itself a finding.** If a check cannot run, its findings stay as they were. nats-lens then raises `system-001` "Check failed to run", which becomes critical when the same check has failed ten collections in a row. Each check has `checks.timeout` (5 seconds) to run, and all checks of a collection share half the collect interval. The order of the checks rotates by one every collection, so an overrun does not always leave out the same checks.
- **Mutes hide a finding without losing it.** A mute covers one check, either for one entity or for every entity. It needs a reason of at most 1000 characters and an end between one minute and 90 days ahead, and records who set it: the verified IAP user behind IAP, otherwise the name given in the request. A muted finding still records its transitions. The UI shows it apart from the unmuted findings, it never counts towards the overview's status, and it is exported to `/metrics` with `muted="true"`. A mute can be lifted early, and nats-lens records who lifted it. Lifted and expired mutes stay as history.
- **Accept one entity's finding as by design.** An acceptance is a mute of one entity's finding (`MuteFinding` with `accept`) with a review date, between a day and 365 days ahead, as its end, and the same reason and author rules. It reads as muted everywhere mutes are read, and as accepted in the UI ([triage-and-mutes.md](triage-and-mutes.md)); the Capacity page accepts `stream-005` ([capacity.md](capacity.md)). For a stream or a server, nats-lens records the entity's growth over the last week (the median, over the last day, of the change since a week earlier divided by 7, so daily and weekly patterns cancel), and after each check run ends the acceptance when that growth now differs by more than half of the recorded rate, or of 1 MiB a day when that is larger, recording "growth changed from … to …" as the reason. An entity with under about 8 days of history gets its baseline recorded later, the first time it can be measured. A purge or a restore changes the week's growth and also ends it, so the finding returns for a new decision. At the review date the acceptance ends as a mute does. The review needs a `retention` of at least 8 days and an hour; with less, acceptances record no growth and end only at their review date, and nats-lens logs a warning at start.

## Scenarios

### A problem raises a finding and resolves it

- **Surfaces:** `ui`
- **Given** a stream with three replicas, two of them on servers that are down
- **When** nats-lens collects twice in a row and sees no quorum
- **Then** `stream-001` "Stream quorum lost" fires as critical for that stream, and its detail lists each reporting server's view of the leader
- **When** the servers come back and the stream has a leader again
- **Then** the finding resolves, and its transitions show when it fired and when it resolved

### A rolling restart stays quiet

- **Surfaces:** `ui`
- **Given** a cluster of three servers
- **When** the servers restart one at a time, for example during a node upgrade
- **Then** each restart is an info finding `server-004` "Server restarted", whose detail says whether it was an upgrade or a configuration change
- **And** no quorum, agreement or missing-asset finding is raised for the restarting server's reports

### Read what a check looks for

- **Surfaces:** `cli`
- **Given** nats-lens is installed
- **When** I run `natslens checks list -config natslens.yaml`
- **Then** I see every check with its code, name, entity, category, severity, kind, lookback and thresholds, with the values in effect after my configuration, and the state `on` or `disabled`
- **And** a severity marked `+` changes per finding by rule

### Tune a threshold

- **Surfaces:** `cli`
- **Given** a consumer whose processing pauses for up to 10 minutes by design
- **When** I set `checks.thresholds.consumer-006.max_idle: 10m` in the configuration file and restart nats-lens
- **Then** `consumer-006` "No delivery while messages wait" fires only after 10 minutes without a delivery
- **And** a typo in the check code or threshold name stops nats-lens at start with an error that names it

### Add a runbook link to a check

- **Surfaces:** `cli`, `ui`
- **Given** a remediation file that maps `consumer-010` to "Runbook: https://runbooks.example.com/nats/missing-durable."
- **When** I set `checks.remediation_file` to it, restart nats-lens, and open a `consumer-010` finding
- **Then** the remediation shows the check's own text, a `---` line, and then my text
- **And** a code in the file that names no check stops the start

### Try a check change on a copy of the data

- **Surfaces:** `cli`
- **Given** a copy of a nats-lens database that no collector has open
- **When** I run `natslens checks run -data-dir DIR`, optionally with `-epoch N`
- **Then** nats-lens evaluates the checks on that collection, read-only, and prints the findings
- **And** it stores nothing

### Investigate a finding

- **Surfaces:** `ui`
- **Given** a firing finding
- **When** I open it from the Overview or the Findings page
- **Then** I see its severity, detail and value, when it was first seen and since when the condition holds, the remediation, the related settings with their values, its transitions, and every check for its entity on a status timeline

### Mute a finding I accept

- **Surfaces:** `ui`, `api`
- **Given** `stream-005` "Stream without any limit" for an archive stream that must keep everything
- **When** I mute the finding with a reason and an end in 30 days, in the UI or with `MuteFinding`
- **Then** the finding moves to the muted findings and no longer affects the Overview's status
- **And** when the mute ends, the finding counts again if the condition still holds

### Accept a finding through the API

- **Surfaces:** `api`
- **Given** `stream-005` fires for `order-archive`, which keeps everything on purpose
- **When** I call `MuteFinding` with the check code, the stream as entity, a reason, a review date six months ahead and `accept`
- **Then** `ListMutes` returns it with `accepted` and the growth nats-lens recorded, and the finding counts as muted until the review date
- **And** a request with `accept` and no entity, or a review date more than 365 days ahead, is refused

## Planned

- **Accept more than one entity.** Acceptance of the same finding on related entities (for example every consumer of one stream), or of the check everywhere, and accepted findings listed together under "Review accepted findings". Mutes stay for short-term silencing.
- **One cause, one card.** Findings of one check that share a cause, such as four consumers of one stream with the same creation-time difference, are grouped into one item on the Overview.
- **Notifications.** Sending firing and resolved findings to a channel is planned in [notifications.md](notifications.md).
- **More checks.** Each check catalogue lists its planned checks.

## Not supported / out of scope

- **No custom checks without a rebuild.** The checks are built into the binary. Adding a check means adding a SQL file and building nats-lens again.
- **No per-entity thresholds.** A threshold applies to every entity of its check. Use a mute for a single entity that needs a different rule.
- **Checks never change NATS.** A check only reads what nats-lens collected. nats-lens never restarts, purges, removes a peer or edits configuration; the remediation tells a person what to do.
- **Muting does not stop a check.** The check keeps running and recording transitions. To stop a check, turn it off with `checks.disabled`.
- **No mute without an end.** Every mute ends within 90 days.
- **No Markdown in remediation text.** Only backticks mark code; links show as plain text.
- **No per-subject checks.** NATS servers do not report traffic per subject, so no check judges the rate of a single subject.
- **`natslens checks run` does not catch up.** It evaluates one stored collection for trying a check; it does not store findings for missed collections.

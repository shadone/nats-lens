# System checks

- **Surfaces:** `ui`, `cli`
- **Status:** shipped
- **Related:** [checks.md](checks.md), [server-checks.md](server-checks.md), [metrics-and-alerting.md](metrics-and-alerting.md)

## What it does

The system checks judge what belongs to no single server: the JetStream meta group, which places and changes every stream and consumer, the JetStream servers as a whole, and nats-lens's own health (its check engine, its collections, its archive and its event capture). A system finding has no entity key; it is about the whole system. There are 9 system checks, `system-001` to `system-009`. The general rules of findings, thresholds and mutes are in [checks.md](checks.md).

## The checks

### `system-001` Check failed to run

- **Problem:** one or more checks returned an error or ran out of time in this collection. Their findings keep the state of their last successful run, so a problem they look for can go unreported.
- **Fires when:** any check failed in this collection. The detail lists the checks that failed and their errors.
- **Severity:** warning. Critical once the same check has failed ten collections in a row.
- **Thresholds:** none of its own. Each check may run for `checks.timeout` (5 seconds), and all checks of a collection share half the collect interval; a check that would start after that budget is a failed run.
- **Left out:** checks turned off with `checks.disabled`, as they do not run and so cannot fail. This check is built into the engine and has no SQL file. It can itself be turned off with `checks.disabled`; failed checks then show only in the log and in the `nats_lens_check_failing` metric.

### `system-002` No JetStream meta leader

- **Problem:** the JetStream meta group has no leader. Without one, no stream or consumer can be created, deleted or updated, and assets cannot move to other servers.
- **Fires when:** none of the clustered JetStream servers reports a meta leader, or the meta leader reports more of the meta group's peers offline than its quorum allows.
- **Severity:** critical.
- **Thresholds:** none.
- **Left out:** single-server deployments and servers without JetStream, which have no meta group.

### `system-003` JetStream meta leader changing often

- **Problem:** the meta leader keeps changing. Each change pauses JetStream API calls for the election.
- **Fires when:** the meta leader changed more than `max_changes` times within the lookback, counted from the `meta.leader_elected` advisories and from the leader the servers report in each collection, whichever count is higher.
- **Severity:** warning.
- **Thresholds:** lookback 15m, `max_changes` 4.
- **Left out:** the first change away from a leader within two collect intervals after it announced lame duck mode or shutdown, or after its process went away and was replaced. A rolling restart moves the leader once per restarted leader, and that hand-over is not counted. Later changes away from the same server count.

### `system-004` JetStream meta group falling behind

- **Problem:** the meta group does not keep up. Creating, updating and deleting streams and consumers is slow or times out, and a server that is not current cannot take over as meta leader.
- **Fires when:** in every collection for at least `min_duration`, the meta leader sees peers that answer but are not current: they have not stored the meta group's latest operations. Only the leader's view of its peers counts, as a follower does not track the other followers. The hold must start from a collection between `min_duration` and twice `min_duration` ago, so behind on both sides of a long gap in collection does not fire. A collection without the leader's own report (its JSZ timed out, or no server claimed the lead during an election) is unknown: it neither breaks the hold nor counts towards resolving. A firing finding resolves once no collection of the last `min_duration` was behind.
- **Severity:** warning (proposed, not yet confirmed).
- **Thresholds:** `min_duration` 5m (proposed, not yet confirmed).
- **Left out:** offline peers, which `server-001` and `system-002` report; collections in which a server is restarting, within `checks.restart_window` of its start, or in which a server new to nats-lens joined within `checks.restart_window`, as such a server catches up the meta group first; JSZ's meta `pending`, which counts the JetStream API requests queued on a server, not meta operations behind; servers without a clustered meta group. `server-012` reports a raft group, the meta group among them, that one server keeps catching up; this check reports the meta group as its leader sees it.

### `system-005` JetStream meta group size differs from the servers

- **Problem:** a JetStream server is not a voting member of the meta group, or does not know its current peers, for example after a peer was removed by hand while its server kept running, or after a server joined with a stale state.
- **Fires when:** in every collection for at least `min_duration`, more JetStream servers answer than the meta group has peers in the meta leader's view, or an answering JetStream server reports a meta group of another size than the leader. The detail names each server that differs, with the size and leader it reports. As with `system-004`, the hold must start between `min_duration` and twice `min_duration` ago, and a collection in which no server reports itself as meta leader is unknown, neither breaking the hold nor counting towards resolving. A firing finding resolves once no collection of the last `min_duration` differed.
- **Severity:** warning (proposed, not yet confirmed).
- **Thresholds:** `min_duration` 10m (proposed, not yet confirmed).
- **Left out:** servers that do not answer, which `server-001` reports, also when the meta group still lists them as peers; servers without a clustered meta group; a restarting server's own report and that of a server new to nats-lens within `checks.restart_window` of its first collection, and every collection in which one is. Servers are compared within their JetStream domain, so a leaf node domain with its own meta group is no fault.

### `system-006` Mixed JetStream API levels

- **Problem:** the servers implement different JetStream API levels. A request for a newer API feature, such as a stream or consumer setting that came with a later release, then works or fails depending on the server that handles it.
- **Fires when:** the answering JetStream servers of one JetStream domain report more than one API level in every collection for at least `min_duration`, the hold starting between `min_duration` and twice `min_duration` ago. The detail names the domain, when it has one, and lists the servers per level; level 0 reads "none reported", a release before API levels. A firing finding resolves once no collection of the last `min_duration` was mixed.
- **Severity:** warning (proposed, not yet confirmed).
- **Thresholds:** `min_duration` 30m (proposed, not yet confirmed).
- **Left out:** collections in which a server is restarting, so a rolling upgrade that keeps moving does not fire, while one that stops half-way fires `min_duration` after its last restart's `checks.restart_window`, and an upgrade whose restarts are more than about 40 minutes apart (`checks.restart_window` plus `min_duration`) fires between them; servers with JetStream disabled. Versions that differ but share one API level do not fire; `server-005` reports every version other than the majority's, at info.

### `system-007` Collections often incomplete

- **Problem:** many of nats-lens's recent collections missed replies, so the checks judge on incomplete data: a finding that needs the missing replies can be missed, or resolve and fire again.
- **Fires when:** at least `warn_share` of the collections in the lookback were incomplete: a server that answered left a monitoring request unanswered or answered it with an error, or nats-lens itself failed during the collection (a request it could not publish, a reply it could not read). The notes nats-lens adds to a collection's errors, such as a value a newer nats-server sent, are not failures and do not count. The detail names the three requests that failed most, per server, with the latest error, and the share. A firing finding stays until the share falls below `resolve_ratio` of `warn_share`.
- **Severity:** warning (proposed, not yet confirmed).
- **Thresholds:** lookback 30m, `warn_share` 0.25, `min_collections` 10, `resolve_ratio` 0.5 (proposed, not yet confirmed).
- **Left out:** servers that answered nothing, which `server-001` reports; the replies of a server within `checks.restart_window` of its start, or after it announced lame duck mode or shutdown within `checks.restart_window`, so a rolling restart does not add up; a lookback with fewer than `min_collections` collections, after a short history or a gap.

### `system-008` Archive export behind

- **Problem:** nats-lens cannot export closed hours to its Parquet archive. Until the exports succeed again, a database that is lost or cannot be opened is rebuilt without those hours.
- **Fires when:** a closed hour that holds collections or events has waited longer than `max_delay` for its export, counted from the end of the hour, or from the start of nats-lens when that is later. The detail names the oldest hour waiting, how many later closed hours wait too, and the time of the last export. The finding resolves once every closed hour is exported.
- **Severity:** warning (proposed, not yet confirmed).
- **Thresholds:** `max_delay` 1h (proposed, not yet confirmed).
- **Left out:** the open hour, which is exported provisionally; `natslens once`, which does not export, and `natslens checks`, which judges a stored database without an archiver. An hour exported only provisionally before it closed still waits.

### `system-009` Event messages dropped

- **Problem:** nats-lens received event messages and could not store them, so checks and pages that read events (leader elections, lame duck and shutdown announcements, deleted consumers, client disconnects) may miss what those messages said.
- **Fires when:** the `collector.messages_dropped` events received within the lookback count more than `max_dropped` messages. A slow consumer report on an events connection carries no count and counts as one, and the detail then says "at least". The detail gives the latest event's summary. The finding resolves once the lookback holds no more than `max_dropped`.
- **Severity:** warning (proposed, not yet confirmed).
- **Thresholds:** lookback 1h, `max_dropped` 0 (proposed, not yet confirmed).
- **Left out:** times when events were not captured at all (`collector.not_connected`, a stopped nats-lens); those are gaps, not dropped messages, and Replay and the Accounts page show them.

## Behavior and rules

- **A failed check is not silence.** Because `system-001` fires whenever a check fails, a check that cannot run never looks like a check that found nothing. The `/metrics` endpoint also exports `nats_lens_check_failing` per check.
- **Checks rotate.** The order of the checks moves by one every collection, so when the budget runs out, a different check is left out each time.
- **System findings carry no entity.** In the UI they read as "system". A mute of a system finding, for "This entity" or for every entity of the check, covers the whole system.
- **Held conditions do not flap.** `system-004`, `system-005` and `system-006` fire only when their condition held in every collection for `min_duration`, and a firing finding resolves only after `min_duration` without it. A collection with a restarting server counts as clean, so a rolling restart or upgrade does not fire them.
- **nats-lens judges itself.** `system-007` to `system-009` are about nats-lens, not NATS: their remediations name nats-lens's own settings (`timeout`, `archive_dir`, `data_dir`, `events.api_audit`) as well as `nats` commands.

## Scenarios

### A check query times out

- **Given** a check whose query takes longer than `checks.timeout`
- **When** nats-lens runs the checks after a collection
- **Then** `system-001` "Check failed to run" fires as a warning, naming that check and its error
- **And** the check's own findings stay as they were

### A check keeps failing

- **Given** the same check fails in ten collections in a row
- **When** nats-lens runs the checks again
- **Then** `system-001` is critical

### Two of three servers are down

- **Given** a three-server JetStream cluster with two servers down
- **When** nats-lens collects
- **Then** `system-002` "No JetStream meta leader" fires as critical
- **And** `server-001` is critical for the missing servers

### A rolling restart moves the meta leader

- **Given** three servers restarted one at a time, each leader announcing lame duck mode first
- **When** the meta leader moves once per restarted leader
- **Then** `system-003` does not fire

### A peer of the meta group stays behind

- **Given** a three-server JetStream cluster whose meta leader sees one answering peer not current
- **When** that lasts for every collection of 5 minutes, with no server restarting
- **Then** `system-004` "JetStream meta group falling behind" fires as a warning, naming the leader's view
- **And** it resolves once no collection of the last 5 minutes was behind

### A removed peer keeps running

- **Given** a meta group of three peers and a fourth JetStream server that answers but was removed from the group by hand
- **When** nats-lens has seen it so for 10 minutes
- **Then** `system-005` fires, naming the fourth server and the meta group size it reports

### A rolling upgrade stops half-way

- **Given** three servers on JetStream API level 1, and one of them upgraded to a release with level 2
- **When** the other two are not upgraded within 30 minutes after the upgraded server's restart window
- **Then** `system-006` "Mixed JetStream API levels" fires, listing the servers per level
- **And** a rolling upgrade that restarts the next server before then does not fire it

### A server keeps leaving a request unanswered

- **Given** a server that answers every collection but leaves RAFTZ unanswered in 16 of the last 61 collections
- **When** nats-lens runs the checks
- **Then** `system-007` "Collections often incomplete" fires as a warning: "16 of 61 collections in the last 30m were incomplete (26%): RAFTZ from nats-1 unanswered in 16."

### The archive volume is full

- **Given** nats-lens running for days, whose archive directory has no space left since 10:00
- **When** the hour 10:00 to 11:00 has waited more than an hour after its end for its export
- **Then** `system-008` "Archive export behind" fires, naming the hour and the time of the last export
- **And** it resolves once the exports catch up

### Event capture falls behind

- **Given** nats-lens dropped 1,200 event messages at 10:10 because its queue was full
- **When** it runs the checks within the following hour
- **Then** `system-009` "Event messages dropped" fires as a warning with the count and the event's summary

### Mute a system finding

- **Given** a firing `system-008` finding during planned maintenance of the archive volume
- **When** I mute it for "This entity: system"
- **Then** the mute is stored and the finding reads as muted

## Planned

Every system check planned earlier is built. A check of the Kubernetes watcher's health is out of scope, below.

## Not supported / out of scope

- **No supercluster-wide checks.** Gateways between clusters are not judged yet; see the planned server checks and [topology.md](topology.md).
- **Accounts are judged elsewhere.** An account's limits and JWTs are the account checks; see [account-checks.md](account-checks.md).
- **No check on the Kubernetes watcher's health.** Watch gaps are recorded as events, not findings.

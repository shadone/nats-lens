# System checks

- **Surfaces:** `ui`, `cli`
- **Status:** shipped
- **Related:** [checks.md](checks.md), [server-checks.md](server-checks.md), [metrics-and-alerting.md](metrics-and-alerting.md)

## What it does

The system checks judge what belongs to no single server: the JetStream meta group, which places and changes every stream and consumer, and nats-lens's own check engine. A system finding has no entity key; it is about the whole system. There are 3 system checks, `system-001` to `system-003`. The general rules of findings, thresholds and mutes are in [checks.md](checks.md).

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

## Behavior and rules

- **A failed check is not silence.** Because `system-001` fires whenever a check fails, a check that cannot run never looks like a check that found nothing. The `/metrics` endpoint also exports `nats_lens_check_failing` per check.
- **Checks rotate.** The order of the checks moves by one every collection, so when the budget runs out, a different check is left out each time.
- **System findings carry no entity.** In the UI they read as "system", and a mute of a system check covers the whole system.

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

## Planned

These system checks are not built. Each line says what it would detect and what data it needs.

- **Meta group falling behind.** The meta group has many pending operations, or peers that are not current. Needs: the meta group's pending count and peers not current from JSZ, stored today.
- **Meta group size differs from the servers.** The meta group lists a different number of peers than the JetStream servers that answer, for example after a peer was removed by hand. Needs: the meta group size from JSZ and the answering servers, stored today.
- **Mixed JetStream API levels.** Servers report different JetStream API levels, so newer API features work on some servers only. Needs: the API level from JSZ, stored today.
- **Collections often partial.** Many of nats-lens's recent collections missed a server or a report, so the checks judge on incomplete data. Needs: the partial flag and the endpoint outcomes of each collection, stored today.
- **Archive export failing.** nats-lens could not export closed hours to its archive for a while, so a lost database would lose more than a few minutes. Needs: the export state per hour, stored today.
- **Events dropped.** nats-lens dropped event messages, so event-based checks may miss something. Needs: the `collector.messages_dropped` events, stored today.

## Not supported / out of scope

- **No supercluster-wide checks.** Gateways between clusters are not judged yet; see the planned server checks and [topology.md](topology.md).
- **Accounts are judged elsewhere.** An account's limits and JWTs are the account checks; see [account-checks.md](account-checks.md).
- **No check on the Kubernetes watcher's health.** Watch gaps are recorded as events, not findings.

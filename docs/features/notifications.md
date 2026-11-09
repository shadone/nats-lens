# Notifications

- **Surfaces:** `api`, `cli`
- **Status:** shipped
- **Related:** [metrics-and-alerting.md](metrics-and-alerting.md), [checks.md](checks.md), [triage-and-mutes.md](triage-and-mutes.md), [mobile.md](mobile.md), [design.md](../design.md)

## What it does

nats-lens sends findings to configured webhook and Slack-compatible channels. Acknowledgements carry the actor, note, and quiet time to the channel where the finding appears. Delivery is read-only towards NATS: only the configured HTTP receivers receive requests. [docs/notifications.md](../notifications.md) documents configuration and the webhook contract.

## Behavior and rules

- **Channels and filters.** The shared configuration lists webhook or Slack-compatible channels. Each channel selects systems, checks, categories, severities and message kinds. The default minimum severity is warning. Empty filter lists select everything; exclusions take precedence. Filtering a kind drops that message without delaying the channel's comparison state.
- **Scheduled delivery.** Only `natslens run` schedules finding delivery. It queues changes after checks store their results. `once` and `checks run` send nothing. The next `run` compares the findings as they stand with each channel's recorded view, rather than replaying every transition during downtime.
- **One baseline.** A channel's first collection sends one summary counting its selected, unmuted firing findings, rather than one message per finding. Adding a channel or rebuilding the database from the archive starts a baseline. Pending deliveries survive an ordinary restart, but notification state and queued messages are not archived.
- **Destination test.** `natslens notify test` sends a test message to all configured channels or one selected channel, without opening a database or connecting to NATS. It reports each outcome and exits unsuccessfully if any channel fails.
- **Links.** With `notifications.ui_url`, messages link the finding and its system. Without it, messages carry no UI link and `run` warns at start.
- **Secrets.** Destination URLs and bearer tokens can come from files or environment variables, read at each attempt. A rotated file applies without restart. Unreadable secrets warn at start and deliveries retry; invalid configuration stops startup. Destinations require HTTPS, except loopback HTTP; redirects fail. Logs and stored delivery outcomes omit secrets.
- **Firing, changes and resolves.** After the baseline, a finding entering the channel's view sends `firing`. A severity change sends `changed`; a fall below the minimum closes the channel's view with `resolved`. An actual resolve closes a known episode. Messages include the check, entity and available detail. Links require `ui_url`.
- **Reminders.** Regular reminders are disabled by default. `repeat_every` of at least 1 hour repeats a finding still firing, unless quiet or flapping suppresses it.
- **Mutes and acceptances.** Muting or accepting a known firing finding sends `muted`, including the actor, reason and end. Further changes stay quiet while covered. When the mute or acceptance ends, a still-selected firing finding re-enters with `firing` and the reason for its return.
- **Flapping is held back.** A flapping finding sends one message that says it flaps, not one per transition. A known acknowledged episode still sends its actual resolve during quiet and after release or expiry, including after restart. Later unacknowledged episodes keep the flapping hold. Each channel remembers only acknowledgements observed in its own view or sent as notes. On upgrade, ambiguous older history does not count as a known acknowledgement.
- **Delivery and retries.** Each channel delivers ordered batches independently. A failed head batch holds that channel's later messages. A 2xx response succeeds; network failures, timeouts, 5xx, 408 and 429 retry. Redirects and other statuses are refused. Retry waits start at 10 seconds and double to 10 minutes; `Retry-After` on 429 or 503 can extend the wait up to that cap. After `give_up_after` (24 hours by default), a still-failing batch becomes failed and the channel moves on.
- **Duplicates and batching.** Delivery is at least once: a crash after the receiver accepts a request can resend it. Webhooks carry stable message deduplication keys and a batch `Idempotency-Key`. A batch contains at most 100 messages; Slack text displays at most 10, with a count and Findings link for the rest when `ui_url` is set. Successful delivery waits for another collection before the next batch.
- **Failures are visible.** Four delivery gauges show delivered and failed messages in the last hour, pending messages, and the oldest pending age. `system-010` reports old pending messages or recent terminal failures ([system-checks.md](system-checks.md)). A failing channel cannot report its own failure reliably; monitor metrics or another channel.
- **Acknowledgements keep the finding visible.** An acknowledgement of a finding in a channel's view sends the actor and note, with a quiet end or “until resolved”. Findings outside the view send no acknowledgement.
- **Quiet limits repeated messages.** Quiet suppresses reminders and downward severity changes, including a fall below the channel's severity threshold. Rising severity and actual resolves still send.
- **Quiet ends once.** Expiry or release sends a reminder with reason “acknowledgement quiet ended” if the known finding still fires, even with regular reminders disabled. A finding below the severity threshold also leaves the view with a resolved message.
- **Replacements and gaps.** Each replacement in the channel's view sends its note. A replacement that extends quiet does not send a quiet-ended reminder. After a gap, a still-selected firing episode sends the acknowledgement notes created since that channel's previous collection, and at most one quiet-ended reminder. An unchanged acknowledgement does not replay after restart.

## Scenarios

### Send critical findings to a webhook

- **Surfaces:** `cli`
- **Given** a baselined channel for critical findings with a webhook URL and `ui_url` in the configuration
- **When** `stream-001` fires as critical
- **Then** the webhook receives a message with the check, the stream and the detail
- **And** it links the finding and receives a second message when the finding resolves

### A muted finding stays quiet

- **Surfaces:** `api`
- **Given** a finding muted with `MuteFinding` until tomorrow
- **When** it changes severity
- **Then** no channel receives a message

### Acknowledge a finding in a channel

- **Surfaces:** `api`, `cli`
- **Given** a channel knows a firing finding
- **When** an operator acknowledges that episode through the API
- **Then** the channel receives the stored actor, note, and quiet time
- **And** quiet suppresses reminders and downward severity changes while rises and actual resolves still send

### Hand a finding back

- **Surfaces:** `api`, `cli`
- **Given** an acknowledgement quiets a finding in a channel's view and regular reminders are disabled
- **When** the operator releases the acknowledgement or its quiet expires
- **Then** the channel receives one reminder with reason “acknowledgement quiet ended”
- **And** restarting nats-lens does not replay that reminder or an unchanged acknowledgement

### An acknowledged flapping episode resolves after hand-back

- **Surfaces:** `api`, `cli`
- **Given** a channel knows an acknowledged firing episode that is flapping
- **When** quiet ends, nats-lens restarts, and the same episode resolves while still marked as flapping
- **Then** the channel receives one resolved message
- **And** a later unacknowledged episode keeps the ordinary flapping hold

### A channel starts after an acknowledgement was released

- **Surfaces:** `api`, `cli`
- **Given** an acknowledgement ended before a channel's baseline
- **When** that channel observes the same episode flapping and then resolving
- **Then** the channel holds the resolve until flapping ends
- **And** an inactive channel's older history does not change that behavior

### Start a channel without a burst

- **Surfaces:** `cli`
- **Given** a configured channel has no saved notification state and three selected findings fire
- **When** `natslens run` stores its first checked collection
- **Then** the channel receives one baseline summary counting three findings
- **And** it receives later changes rather than three opening messages

### Retry a failed receiver independently

- **Surfaces:** `api`, `cli`
- **Given** two channels and one receiver returns HTTP 503
- **When** `natslens run` attempts delivery
- **Then** the failed channel retries its oldest batch while the other channel continues
- **And** `/metrics` reports its pending messages and oldest pending age

### Test a destination beside a running collector

- **Surfaces:** `cli`
- **Given** a configuration with a channel named `ops-webhook`
- **When** I run `natslens notify test -config natslens.yaml -channel ops-webhook`
- **Then** the channel receives a test message without opening the collector's database or connecting to NATS
- **And** the command reports success only if the receiver accepts the message

## Not supported / out of scope

- **No paging schedule.** nats-lens does not decide who is on call; the receiving system does.
- **No delivery while nats-lens is down.** A stopped nats-lens sends nothing; the `NatsLensNotCollecting` alert rule covers that case.
- **No templates, e-mail or paging-service API.** Receivers can transform the documented webhook JSON. Slack-compatible delivery sends plain text.
- **No notification UI or quiet hours.** Channels are configured in YAML; there is no channel status page or per-channel quiet schedule.
- **No exactly-once delivery or archive recovery of the queue.** Receivers must deduplicate retries. A rebuilt database starts a baseline instead of restoring pending deliveries.
- **Separate Prometheus quiet rules.** Notification quiet applies to channels. The finding rules in [docs/metrics.md](../metrics.md) explicitly exclude quiet acknowledgements through their metric; notification delivery does not alter a deployment's Prometheus rules ([metrics-and-alerting.md](metrics-and-alerting.md)).

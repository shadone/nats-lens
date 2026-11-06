# Notifications

- **Surfaces:** `api`, `cli`
- **Status:** partial — webhook and Slack delivery includes acknowledgements; the full notification guide is deferred
- **Related:** [metrics-and-alerting.md](metrics-and-alerting.md), [checks.md](checks.md), [triage-and-mutes.md](triage-and-mutes.md), [mobile.md](mobile.md), [design.md](../design.md)

## What it does

nats-lens sends findings to configured webhook and Slack-compatible channels. Acknowledgements carry the actor, note, and quiet time to the channel where the finding appears. The broader notification guide remains incomplete.

## Behavior and rules

- **Channels in the configuration file.** Each channel has a destination (for example a webhook URL) and the severities it receives.
- **Firing and resolved.** A channel receives a message when a finding fires or changes severity, and when it resolves, with the check, entity, detail and a link to the finding's page.
- **Mutes are honoured.** A muted finding sends nothing while the mute is in force.
- **Flapping is held back.** A flapping finding sends one message that says it flaps, not one per transition. A known acknowledged episode still sends its actual resolve after release or expiry, including after restart. Later unacknowledged episodes keep the flapping hold. Each channel remembers only acknowledgements observed in its own view or sent as notes.
- **Failures are visible.** A message that cannot be delivered is retried, and repeated failures show in nats-lens's own health metrics.
- **Acknowledgements keep the finding visible.** An acknowledgement of a finding in a channel's view sends the actor and note, with a quiet end or “until resolved”. Findings outside the view send no acknowledgement.
- **Quiet limits repeated messages.** Quiet suppresses reminders and downward severity changes, including a fall below the channel's severity threshold. Rising severity and actual resolves still send.
- **Quiet ends once.** Expiry or release sends a reminder with reason “acknowledgement quiet ended” if the known finding still fires, even with regular reminders disabled. A finding below the severity threshold also leaves the view with a resolved message.
- **Replacements and gaps.** Each replacement sends its note. A replacement that extends quiet does not send a quiet-ended reminder. After a gap, the channel receives the current episode's acknowledgement notes and at most one quiet-ended reminder. An unchanged acknowledgement does not replay after restart.

## Scenarios

### Send critical findings to a webhook

- **Surfaces:** `cli`
- **Given** a channel for critical findings with a webhook URL in the configuration
- **When** `stream-001` fires as critical
- **Then** the webhook receives a message with the check, the stream, the detail and a link to the finding
- **And** it receives a second message when the finding resolves

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

## Not supported / out of scope

- **No paging schedule.** nats-lens does not decide who is on call; the receiving system does.
- **No delivery while nats-lens is down.** A stopped nats-lens sends nothing; the `NatsLensNotCollecting` alert rule covers that case.

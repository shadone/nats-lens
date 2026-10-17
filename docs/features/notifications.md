# Notifications

- **Surfaces:** `api`, `cli`
- **Status:** planned
- **Related:** [metrics-and-alerting.md](metrics-and-alerting.md), [checks.md](checks.md), [triage-and-mutes.md](triage-and-mutes.md), [mobile.md](mobile.md), [design.md](../design.md)

## What it does

Planned; nothing here is built yet. nats-lens will send findings to a channel, such as a webhook, when they fire and when they resolve, for deployments that have no Prometheus-based alerting. Today findings reach an alerting system only through `/metrics` ([metrics-and-alerting.md](metrics-and-alerting.md)), and the design keeps alert delivery out of the first release. All scenarios below describe intended behaviour.

## Behavior and rules

- **Channels in the configuration file.** Each channel has a destination (for example a webhook URL) and the severities it receives.
- **Firing and resolved.** A channel receives a message when a finding fires or changes severity, and when it resolves, with the check, entity, detail and a link to the finding's page.
- **Mutes are honoured.** A muted finding sends nothing while the mute is in force.
- **Flapping is held back.** A flapping finding sends one message that says it flaps, not one per transition.
- **Failures are visible.** A message that cannot be delivered is retried, and repeated failures show in nats-lens's own health metrics.

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

## Not supported / out of scope

- **No paging schedule.** nats-lens does not decide who is on call; the receiving system does.
- **No delivery while nats-lens is down.** A stopped nats-lens sends nothing; the `NatsLensNotCollecting` alert rule covers that case.

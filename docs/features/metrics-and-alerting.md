# Metrics and alerting

- **Surfaces:** `api`
- **Status:** shipped
- **Related:** [checks.md](checks.md), [triage-and-mutes.md](triage-and-mutes.md), [notifications.md](notifications.md), [running-nats-lens.md](running-nats-lens.md), [design.md](../design.md)

## What it does

nats-lens does not send alerts itself. Instead, `natslens run` serves its findings and its own health as Prometheus metrics at `/metrics`, so that an alerting system that already exists (a Prometheus with Alertmanager, or Cloud Monitoring with Managed Prometheus) can page on them. A mute set in the UI changes the metrics, so an alert rule written as the project README suggests stops matching a muted finding.

## Behavior and rules

- **Three series per firing finding.** Each firing finding has `nats_lens_finding` (1, with a `muted` label of `true` or `false`), `nats_lens_finding_severity` (1 info, 2 warning, 3 critical) and `nats_lens_finding_flapping` (1 while it flaps). All three share the identity `code`, `entity_type` and `entity_key`, so a change of severity does not start a new series. A mute does start a new `nats_lens_finding` series, because `muted` is one of its labels.
- **A cap per check.** At most 50 unmuted entities per check get these three series: the worst first, then by entity key, so the set stays stable. `nats_lens_finding_series_omitted` counts the ones left out per check. Muted findings are always exported.
- **Counts that ignore the cap.** `nats_lens_findings_by_check` and `nats_lens_findings_by_check_max_severity` give the firing, unmuted findings of each check and their highest severity; `nats_lens_findings_total` counts them by severity; `nats_lens_findings_muted` counts the muted ones.
- **nats-lens's own health.** `nats_lens_last_epoch_age_seconds` (the age of the newest collection; absent before the first), `nats_lens_last_epoch_partial`, the collections and partial collections of the last hour, the collections of the last hour for which no check ran, the event messages dropped in the last hour, the check runs that failed in the last hour, and `nats_lens_check_failing` per check.
- **Who may scrape.** On a loopback listener, `/metrics` follows the same Host allowlist as the API. On any other listener it is exempt from the allowlist, because Prometheus scrapes by the pod's address. The endpoint names streams, consumers, servers and clients, so such a listener must not be reachable from outside the cluster.
- **Example rules.** The project README gives rules for a critical finding, a warning finding, a collector that stopped collecting, checks that stopped running, and a failing check, both as Prometheus rules and as a Cloud Monitoring alert policy.

## Scenarios

### Page on a critical finding

- **Given** Prometheus scrapes `/metrics` and evaluates the README's `NatsLensCriticalFinding` rule
- **When** `stream-001` fires as critical for two minutes
- **Then** Prometheus raises the alert with the check code and the entity in its labels

### Mute stops the page

- **Given** the alert above is firing
- **When** an operator mutes the finding in the UI
- **Then** `nats_lens_finding` for it now carries `muted="true"`, the rule stops matching, and the alert closes

### Notice that nats-lens stopped collecting

- **Given** the README's `NatsLensNotCollecting` rule
- **When** nats-lens stores no collection for more than three minutes, or its metrics vanish
- **Then** the alert fires, saying that findings are stale

## Not supported / out of scope

- **No alert delivery of its own.** nats-lens sends no email, chat message or webhook; see [notifications.md](notifications.md).
- **No raw NATS metrics.** `/metrics` exports findings and nats-lens's health, not every server, stream or consumer number. A NATS Prometheus exporter is the tool for those.
- **No alert rules shipped as resources.** The rules are examples in the README; a deployment writes its own.

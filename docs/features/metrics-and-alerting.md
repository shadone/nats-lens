# Metrics and alerting

- **Surfaces:** `api`
- **Status:** shipped
- **Related:** [checks.md](checks.md), [triage-and-mutes.md](triage-and-mutes.md), [notifications.md](notifications.md), [running-nats-lens.md](running-nats-lens.md), [design.md](../design.md)

## What it does

Besides sending findings to configured [notification channels](notifications.md), `natslens run` serves its findings and its own health as Prometheus metrics at `/metrics`, so that an alerting system that already exists (a Prometheus with Alertmanager, or Cloud Monitoring with Managed Prometheus) can page on them. A mute set in the UI changes the metrics, so an alert rule written as the project README suggests stops matching a muted finding.

## Behavior and rules

- **Three series per firing finding.** Each firing finding has `nats_lens_finding` (1, with a `muted` label of `true` or `false`), `nats_lens_finding_severity` (1 info, 2 warning, 3 critical) and `nats_lens_finding_flapping` (1 while it flaps). All three share the identity `system`, `code`, `entity_type` and `entity_key`, so a change of severity does not start a new series. A mute does start a new `nats_lens_finding` series, because `muted` is one of its labels.
- **One label per system.** Every sample carries `system`, the ID of the system it describes (for a file without `systems`, its `system_id`, default `default`). Each system is read in parallel, within `metrics_timeout` (8 seconds; keep it below the Prometheus `scrape_timeout`), and at most once at a time: a system whose previous read still runs, fails or panics is left out of the scrape, and `nats_lens_system_scrape_success` is 0 for it and 1 for each system read. The scrape answers 200 while one system is read and 500 when none is, so with a single system a failure is a 500 and no `nats_lens_system_scrape_success 0` line appears.
- **A cap per check.** At most 50 unmuted entities per check get these three series: the worst first, then by entity key, so the set stays stable. `nats_lens_finding_series_omitted` counts the ones left out per check. Muted findings are always exported.
- **Counts that ignore the cap.** `nats_lens_findings_by_check` and `nats_lens_findings_by_check_max_severity` give the firing, unmuted findings of each check and their highest severity; `nats_lens_findings_total` counts them by severity; `nats_lens_findings_muted` counts the muted ones.
- **nats-lens's own health.** `nats_lens_last_epoch_age_seconds` (the age of the newest collection; absent before the first), `nats_lens_last_epoch_partial`, the collections and partial collections of the last hour, the collections of the last hour for which no check ran, the event messages dropped in the last hour, the check runs that failed in the last hour, and `nats_lens_check_failing` per check. The checks `system-007` to `system-009` turn incomplete collections, a stalled archive and dropped event messages into findings ([system-checks.md](system-checks.md)).
- **Who may scrape.** On a loopback listener, `/metrics` follows the same Host allowlist as the API. On any other listener it is exempt from the allowlist, because Prometheus scrapes by the pod's address. The endpoint names streams, consumers, servers and clients, so such a listener must not be reachable from outside the cluster.
- **Notification delivery.** Per system and channel, four gauges report delivered messages and terminal failures in the last hour, pending messages, and the oldest pending age (absent with nothing pending). Refused or given-up messages count from the outcome time, including after a retention sweep. `system-010` reports notification delivery failures ([system-checks.md](system-checks.md)).
- **Example rules.** The project README gives seven Prometheus rules (a critical finding, a warning finding, a collector that stopped collecting, checks that stopped running, a failing check, a system whose metrics cannot be read, and stuck notification delivery) and one Cloud Monitoring alert policy, for the critical finding; it says the other rules translate the same way.

- **Acknowledgement quiet.** `nats_lens_finding_acknowledged` is 1 with the finding identity labels only while quiet holds, and absent otherwise. It follows the same selected entity set as the three finding series. `nats_lens_findings_acknowledged` counts every firing finding whose quiet holds, including muted findings, without that cap. These metrics measure quiet, while the UI acknowledgement tally also counts acknowledgements whose quiet expired.
- **Quiet-aware finding rules.** Both README finding rules and its Cloud Monitoring query add `unless on(system, code, entity_type, entity_key) nats_lens_finding_acknowledged` after excluding mutes. Prometheus annotations URL-encode the labels and link to the finding through the label route. Cloud Monitoring documentation explains constructing the same route.
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
- **When** nats-lens has stored no collection for more than three minutes, or its metrics are absent, and that lasts five minutes
- **Then** the alert fires, saying that findings are stale

### One system's metrics fail

- **Given** two systems, and the database of one cannot be read
- **When** Prometheus scrapes `/metrics`
- **Then** the scrape answers 200 with the other system's series, `nats_lens_system_scrape_success` is 0 for the failing one, and the README's `NatsLensSystemMetricsFailing` rule fires after five minutes

### Notice stuck notification delivery

- **Given** a channel has pending messages and Prometheus evaluates `NatsLensNotificationsStuck`
- **When** the oldest pending message is more than 600 seconds old
- **Then** the rule matches that system and channel
- **And** the age series disappears when no messages remain pending

### Quiet an acknowledged alert

- **Given** Prometheus evaluates a README finding rule for an unmuted firing finding
- **When** an acknowledgement quiets that episode
- **Then** the acknowledgement series appears and the finding rule stops matching
- **And** when quiet ends, the series disappears and the rule can match again, with its configured waiting period

## Not supported / out of scope

- **No automatic quieting of aggregate alerts.** Per-finding exclusions need the identity labels. A deployment alerting on aggregate counts must choose its own quiet policy.
- **No raw NATS metrics.** `/metrics` exports findings and nats-lens's health, not every server, stream or consumer number. A NATS Prometheus exporter is the tool for those.
- **No alert rules shipped as resources.** The rules are examples in the README; a deployment writes its own.

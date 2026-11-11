# Metrics

`natslens run` serves Prometheus metrics at `/metrics` on the API listener, so that the findings reach the alerting system. On a loopback listener the Host allowlist applies to `/metrics` as to the API. On any other listener `/metrics` is exempt from it, because Prometheus scrapes by the pod's address: the endpoint is read only but names streams, consumers, servers and clients, so such a listener must not be reachable from outside the cluster.

Every sample carries a `system` label, the ID of the system it describes (`default` until the configuration lists systems; the API timings below also use `all` and `unknown`), and each family's `# HELP` and `# TYPE` appear once with the samples of every system below them. Each system is read on its own, in parallel and within `metrics_timeout` (8 seconds by default; keep it below the Prometheus `scrape_timeout`, usually 10 seconds, or Prometheus gives up on the whole target), and never twice at once: a scrape that finds the system's previous read still running counts it as failed at once. A system that fails, times out or panics leaves none of its series in the scrape, and the scrape still answers 200 for the others. Only when every system fails does `/metrics` answer 500, so with a single system a failure is a 500 and no `nats_lens_system_scrape_success 0` line appears. The label is new: a series scraped before it is a different series to Prometheus, so a dashboard or rule that joins series must add `system` to its `on(...)` and `by(...)` lists, and a series history starts again at the upgrade.

Every firing finding has three series with one identity, `system`, `code`, `entity_type` and `entity_key`, so that a change of severity does not start a new series. A mute does start a new `nats_lens_finding` series, because `muted` is its label; `nats_lens_finding_severity` and `nats_lens_finding_flapping` keep their identity:

| Metric | Labels | Meaning |
| ------ | ------ | ------- |
| `nats_lens_finding` | identity, `muted` (`true` or `false`) | 1 for each firing finding, muted or not; at most 50 unmuted entities per check, the worst first and then by entity key, so the set is stable; muted findings are always exported |
| `nats_lens_finding_severity` | identity | Severity of the finding: 1 info, 2 warning, 3 critical |
| `nats_lens_finding_flapping` | identity | 1 while the finding flaps |
| `nats_lens_finding_acknowledged` | identity | 1 while acknowledgement quiet holds; absent otherwise; exported only for findings selected by the same entity cap |
| `nats_lens_findings_acknowledged` | | Firing findings whose acknowledgement quiet holds, muted or not, every entity counted |
| `nats_lens_finding_series_omitted` | `code` | Findings of a check left out of the three series by the cap |
| `nats_lens_findings_by_check` | `code` | Firing, unmuted findings of a check, every entity counted |
| `nats_lens_findings_by_check_max_severity` | `code` | Highest severity among them |
| `nats_lens_findings_total` | `severity` | Firing, unmuted findings by severity |
| `nats_lens_findings_muted` | | Firing findings a mute covers |
| `nats_lens_last_epoch_age_seconds` | | Age of the newest epoch; absent before the first |
| `nats_lens_last_epoch_partial` | | 1 when the newest epoch is partial |
| `nats_lens_epochs_last_hour`, `nats_lens_partial_epochs_last_hour` | | Epochs, and partial epochs, stored in the last hour |
| `nats_lens_epochs_without_check_runs_last_hour` | | Epochs of the last hour (the newest interval left out) for which no check ran |
| `nats_lens_event_messages_dropped_last_hour` | | Event messages the collector dropped in the last hour |
| `nats_lens_check_run_errors_last_hour` | | Check runs that failed in the last hour |
| `nats_lens_check_failing` | `code` | 1 for a check whose newest run failed |
| `nats_lens_notifications_delivered_last_hour` | `channel` | Messages delivered in the last hour, counted by outcome time |
| `nats_lens_notifications_failed_last_hour` | `channel` | Messages refused or given up in the last hour, counted by outcome time |
| `nats_lens_notifications_pending` | `channel` | Messages waiting for delivery, including retries |
| `nats_lens_notifications_oldest_pending_seconds` | `channel` | Age of the oldest pending message; absent when none are pending |
| `nats_lens_system_scrape_success` | | 1 when the system's metrics were read for this scrape, 0 when reading them failed, took longer than `metrics_timeout` or found the previous read still running; then the system's other series are absent from that scrape |

nats-lens also times its own work, for finding what is slow and what waits. These series come from counters the process keeps since it started, not from the database, so they are present even when a system's read fails, as long as one system is read. Each is a Prometheus histogram (`_bucket` by `le`, `_sum`, `_count`; buckets from 1 ms to 60 s) or a gauge. Their labels take values from closed sets: system IDs, the API's procedure names, Connect codes and the fixed names below; no label carries an entity, a query or a value read from NATS, and a family holds at most 256 label sets, the rest counted under `other`. See the [performance baseline](operations/performance-baseline.md) for what the numbers look like under load.

| Metric | Labels | Meaning |
| ------ | ------ | ------- |
| `nats_lens_api_request_duration_seconds` | `system`, `procedure`, `code` | Duration of an API call by procedure and Connect code (`ok` for success). `system` is the system the request names, the default system for none, `all` for a request without a system field such as `ListSystems`, and `unknown` for an ID no system has |
| `nats_lens_api_request_queries` | `system`, `procedure` | Database queries and statements one call ran |
| `nats_lens_api_request_query_seconds` | `system`, `procedure` | Seconds one call's queries ran, summed; above the call's duration when its queries ran in parallel |
| `nats_lens_api_requests_in_flight` | `system`, `procedure` | Calls running now |
| `nats_lens_db_query_duration_seconds` | `system`, `source` | One query or statement, from its start until its rows are closed, by the work that ran it: `api`, `metrics`, `epoch_write`, `checks`, `archive`, `retention`, `events`, `notify` or `other` |
| `nats_lens_store_lock_wait_seconds` | `system`, `op` | Time a write waited for the database's write lock, by operation |
| `nats_lens_store_lock_held_seconds` | `system`, `op` | Time a write held the lock; `op="epoch"` is the epoch write. The other operations are `events`, `account_names`, `check_apply`, `loopback`, `retention_sweep`, `archive_record`, `rebuild`, `client_pods`, `inventory`, `mutes`, `acknowledgements`, `acceptances` and `notify` |
| `nats_lens_store_lock_waiters` | `system` | Writes waiting for the lock now |
| `nats_lens_job_duration_seconds` | `system`, `job` | One run of a background job: `collect` (asking the servers), `check_evaluation` (the checks' queries), `checks` (evaluation and storing the outcome), `archive_export` (one hour) and `retention_sweep` |

On GKE, Cloud Monitoring alert policies can evaluate PromQL conditions over the data Managed Service for Prometheus (GMP) collects; the `PodMonitoring` in `k8s/components/gke-iap` scrapes `/metrics`. GMP adds the target labels `project_id`, `location`, `cluster`, `namespace`, `job` and `instance` to every series, so a query can select one deployment with `{cluster="...", namespace="nats-lens"}`. A metric that has a label of one of those names keeps it as `exported_<name>` (`exported_namespace` for a `namespace` label); the natslens metrics have none, so `namespace` is the pod's. An alert policy in Terraform or OpenTofu (one condition shown; the rules below translate the same way):

```hcl
resource "google_monitoring_alert_policy" "nats_lens_critical_finding" {
  project      = var.project
  display_name = "nats-lens critical finding (${var.environment})"
  combiner     = "OR"
  severity     = "CRITICAL"

  conditions {
    display_name = "A critical, unmuted nats-lens finding"
    condition_prometheus_query_language {
      query               = "nats_lens_finding_severity{namespace=\"nats-lens\"} >= 3 unless on(system, code, entity_type, entity_key) nats_lens_finding{namespace=\"nats-lens\", muted=\"true\"} unless on(system, code, entity_type, entity_key) nats_lens_finding_acknowledged{namespace=\"nats-lens\"}"
      duration            = "120s"
      evaluation_interval = "60s"
    }
  }

  notification_channels = [var.notification_channel]

  documentation {
    subject   = "nats-lens finding on ${var.project}"
    content   = "Open [Findings](https://nats-lens.example.com/findings). To open one finding, use /findings/by-labels?system=<encoded system>&code=<encoded code>&entity_key=<encoded entity_key> on that host; URL-encode each label value. Labels: system=$${metric.label.system}, code=$${metric.label.code}, entity_key=$${metric.label.entity_key}."
    mime_type = "text/markdown"
  }
}
```

The same rules in Prometheus form, for a Prometheus with Alertmanager:

```yaml
groups:
  - name: nats-lens
    rules:
      - alert: NatsLensCriticalFinding
        expr: nats_lens_finding_severity >= 3 unless on(system, code, entity_type, entity_key) nats_lens_finding{muted="true"} unless on(system, code, entity_type, entity_key) nats_lens_finding_acknowledged
        for: 2m
        labels: {severity: critical}
        annotations:
          summary: "nats-lens {{ $labels.code }} on {{ $labels.entity_key }} in {{ $labels.system }}"
          description: "Open the finding; acknowledge it while investigating or mute it if it is expected."
          link: "https://nats-lens.example.com/findings/by-labels?system={{ $labels.system | urlquery }}&code={{ $labels.code | urlquery }}&entity_key={{ $labels.entity_key | urlquery }}"
      - alert: NatsLensWarningFinding
        expr: nats_lens_finding_severity == 2 unless on(system, code, entity_type, entity_key) nats_lens_finding{muted="true"} unless on(system, code, entity_type, entity_key) nats_lens_finding_acknowledged
        for: 15m
        labels: {severity: warning}
        annotations:
          link: "https://nats-lens.example.com/findings/by-labels?system={{ $labels.system | urlquery }}&code={{ $labels.code | urlquery }}&entity_key={{ $labels.entity_key | urlquery }}"
      - alert: NatsLensNotCollecting
        expr: nats_lens_last_epoch_age_seconds > 180 or absent(nats_lens_last_epoch_age_seconds)
        for: 5m
        labels: {severity: critical}
        annotations:
          summary: "nats-lens has stored no epoch for 3 minutes; findings are stale"
      - alert: NatsLensChecksFrozen
        expr: nats_lens_epochs_without_check_runs_last_hour > 2
        for: 5m
        labels: {severity: critical}
        annotations:
          summary: "nats-lens stores epochs of {{ $labels.system }} but runs no checks; findings are stale"
      - alert: NatsLensCheckFailing
        expr: nats_lens_check_failing == 1
        for: 10m
        labels: {severity: warning}
      - alert: NatsLensNotificationsStuck
        expr: nats_lens_notifications_oldest_pending_seconds > 600
        labels: {severity: warning}
        annotations:
          summary: "nats-lens notifications to {{ $labels.channel }} in {{ $labels.system }} have waited over 10 minutes"
      - alert: NatsLensSystemMetricsFailing
        expr: nats_lens_system_scrape_success == 0
        for: 5m
        labels: {severity: critical}
        annotations:
          summary: "nats-lens cannot read its data of {{ $labels.system }}; its findings are missing from /metrics"
```

The finding rules above exclude mutes and acknowledgements while their quiet holds. Acknowledging keeps the finding visible and counted at its severity; quiet expiry resumes alert eligibility without ending the acknowledgement. Notification channels apply their own [quiet rules](features/notifications.md). A mute in the UI makes the finding rules stop matching, so the alert closes; the finding's series stay, marked `muted="true"`. Beyond 50 entities of one check, alert on `nats_lens_findings_by_check_max_severity` instead.

Finding links use `/findings/<id>?system=<id>` on every screen. The label route resolves the existing finding, then opens that URL; at 760 px and below it shows the [phone card](features/mobile.md). URL-encode every query value: `+` must be `%2B`, a space can be `+` or `%20`, `/` is `%2F`, and `:` is `%3A`. Unicode values use UTF-8 percent encoding. Both label-route destinations retain the global time range, system, and account. For example, the demo durable opens at [the label link for analytics-aggregator](https://nats-lens.example.com/findings/by-labels?system=example-prod&code=consumer-010&entity_key=APP%2Forder-archive%2Fanalytics-aggregator). Replace the host with the deployment's UI address. The Cloud Monitoring documentation gives the same route as a construction instruction because [Cloud Monitoring documentation variables](https://docs.cloud.google.com/monitoring/alerts/doc-variables) offer substitutions without a documented URL-encoding operation. A missing finding opens Findings filtered to its check with an explanation; other API failures remain errors.

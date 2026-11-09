# Notifications

`natslens run` sends findings to configured webhook and Slack-compatible receivers, alongside Prometheus metrics. It opens no NATS connection for delivery. The [notification capability](features/notifications.md) describes baselines, filters, mutes, flapping, acknowledgement quiet and retries. `once` and `checks run` send nothing; `demo` reads no channel configuration.

A shared configuration block can select critical findings for an online shop's receiver. Store secret URLs and tokens in mounted files or environment variables:

```yaml
notifications:
  ui_url: https://nats-lens.example.com
  channels:
    - name: ops-webhook
      type: webhook
      url_file: /var/run/secrets/natslens/ops-webhook-url
      bearer_token_file: /var/run/secrets/natslens/ops-webhook-token
      min_severity: critical
    - name: ops-chat
      type: slack
      url_env: NATSLENS_OPS_CHAT_URL
      min_severity: warning
```

URL and token sources are read at every attempt. Rotating mounted secret files needs no restart; changing channel settings does. Unreadable secrets warn at start and retry on delivery. Destinations require HTTPS except loopback HTTP, and redirects fail. Allow DNS and outbound HTTPS to each receiver in any egress-restricting NetworkPolicy. The GKE component's policy must be extended for these destinations.

Test all channels, or select one with `-channel`. `-system` sets the system named in the test; without it the command uses the channel's first selected system, or the first configured system. It opens no database and connects to no NATS server, so it can run beside the collector:

```sh
natslens notify test -config natslens.yaml -channel ops-webhook -system example-prod
```

A receiver accepting with HTTP 204 produces `ops-webhook (hooks.example.com): delivered, HTTP 204`. A refusal or retryable failure reports failure, without the secret URL. The command exits with status 1 if any selected channel fails; configuration errors also fail. A test sends once, without the scheduled retry loop.

## Webhook body, version 1

Webhook requests use `POST`, `Content-Type: application/json`, `User-Agent: natslens/<version>`, and `Authorization: Bearer <token>` when configured. `Idempotency-Key` identifies the durable batch by database incarnation, system, channel and first and last outbox row IDs. It stays stable across retries and restarts; a rebuilt database gets a fresh incarnation. Delivery is at least once, so receivers must deduplicate. Scope message keys to the channel and database incarnation when deduplicating across rebuilds.

Times are RFC 3339 UTC to the second. Unset optional fields are omitted; reported numeric zero values remain. Removing, renaming or changing a field's type requires another body version; adding a field does not.

| Field | Type | Meaning |
|---|---|---|
| `version` | number | `1` |
| `sent_at` | time | When the batch was built |
| `system.id`, `system.name` | string | The system the findings belong to; `name` is left out when it is not set |
| `nats_lens_version` | string | nats-lens's version, as in `User-Agent: natslens/<version>` |
| `notifications[]` | array | The messages, in the order they were queued |
| `.kind` | string | `firing`, `changed`, `resolved`, `flapping`, `muted`, `reminder`, `acknowledged`, `baseline` (the one message a channel gets when nats-lens starts notifying it, about no finding), or `test` from `natslens notify test` |
| `.reason` | string | Why, when the kind alone does not say: `mute ended`, `acceptance ended`, `flapping ended`, `severity now info` (or `severity now warning` for a channel with `min_severity: critical`), `no longer selected by the channel`, `acknowledgement quiet ended` |
| `.finding_id` | string | `<check code>:<entity key>`, as the API spells it |
| `.check_code`, `.check_name`, `.category` | string | The check |
| `.severity` | string | `info`, `warning` or `critical`, as the finding stands |
| `.previous_severity` | string | The severity the channel was last sent, on `changed` and severity-based `resolved` messages |
| `.entity_type`, `.entity_key` | string | The entity, as in the API |
| `.entity_name` | string | The entity's readable name, when it has one |
| `.detail` | string | The finding's detail |
| `.since` | time | When the episode began |
| `.condition_since` | time | When the condition began, when the check knows |
| `.transitioned_at` | time | The finding's latest transition |
| `.resolved_at` | time | On `resolved` from a resolve |
| `.flapping` | boolean | `true` while the finding flaps; left out otherwise |
| `.value` | number | The check's value, when it reports one |
| `.mute.accepted`, `.mute.by`, `.mute.reason`, `.mute.until`, `.mute.end_reason` | boolean / string / time | The mute or acceptance, on `muted` and on a `firing` whose mute ended; `until` is an acceptance's review date; `end_reason` is set when nats-lens ended an acceptance |
| `.acknowledgement.by`, `.acknowledgement.note`, `.acknowledgement.quiet_until`, `.acknowledgement.until_resolved` | string / boolean | On `acknowledged`: actor, optional note, and either the quiet end or `until_resolved: true` |
| `.firing_count` | number | On `baseline`: the findings in the channel's view when it started, 0 included |
| `.link` | string | `<ui_url>/findings/<finding id, path-escaped>?system=<id>`; the baseline and test messages link the system's Findings page; left out without `ui_url` |
| `.dedup_key` | string | `<system>:<finding id>:<episode start epoch>:<kind>:<epoch>`, acknowledgement notes append `:<acknowledgement ID>`; the same each time the message is sent; the baseline message's is `<system>:baseline:<epoch>`, the test message's `<system>:test:<Unix microseconds>` |


A firing notification for the shop's order stream has this body:

```json
{
  "version": 1,
  "sent_at": "2026-11-05T09:32:00Z",
  "system": {
    "id": "example-prod",
    "name": "Production"
  },
  "nats_lens_version": "1.4.0",
  "notifications": [
    {
      "kind": "firing",
      "finding_id": "stream-001:APP/order-jobs",
      "check_code": "stream-001",
      "check_name": "Stream without a leader",
      "category": "health",
      "severity": "critical",
      "entity_type": "stream",
      "entity_key": "APP/order-jobs",
      "entity_name": "shop/order-jobs",
      "detail": "no leader for 2m",
      "since": "2026-11-05T09:30:00Z",
      "condition_since": "2026-11-05T09:28:00Z",
      "transitioned_at": "2026-11-05T09:30:00Z",
      "value": 0,
      "link": "https://nats-lens.example.com/findings/stream-001:APP%2Forder-jobs?system=example-prod",
      "dedup_key": "example-prod:stream-001:APP/order-jobs:41230:firing:41230"
    }
  ]
}
```

The other kinds use the same envelope. These message excerpts show the fields that distinguish them; finding fields follow the table above:

```json
[
  {"kind": "changed", "previous_severity": "warning", "detail": "no leader for 2m", "dedup_key": "example-prod:stream-001:APP/order-jobs:41230:changed:41234"},
  {"kind": "resolved", "resolved_at": "2026-11-05T09:32:00Z", "detail": "leader elected", "dedup_key": "example-prod:stream-001:APP/order-jobs:41230:resolved:41234"},
  {"kind": "flapping", "flapping": true, "detail": "no leader for 2m", "dedup_key": "example-prod:stream-001:APP/order-jobs:41230:flapping:41230"},
  {"kind": "muted", "mute": {"by": "alice@example.com", "reason": "migration tonight", "until": "2026-11-05T21:30:00Z"}, "detail": "no leader for 2m", "dedup_key": "example-prod:stream-001:APP/order-jobs:41230:muted:41230"},
  {"kind": "reminder", "detail": "no leader for 2m", "dedup_key": "example-prod:stream-001:APP/order-jobs:41230:reminder:41350"},
  {"kind": "acknowledged", "acknowledgement": {"by": "alice@example.com", "note": "Checking quorum", "quiet_until": "2026-11-05T10:32:00Z"}, "detail": "no leader for 2m", "dedup_key": "example-prod:stream-001:APP/order-jobs:41230:acknowledged:41230:17"},
  {"kind": "baseline", "firing_count": 3, "detail": "nats-lens now notifies this channel: 3 findings firing", "dedup_key": "example-prod:baseline:41230"},
  {"kind": "test", "detail": "nats-lens test message from example-prod", "dedup_key": "example-prod:test:1793871000000000"}
]
```

Slack-compatible channels send only `{"text": ...}`, with a system heading and at most 10 message lines. Larger batches show an additional count and a Findings link when `ui_url` is set. Slack requests have neither `Authorization` nor `Idempotency-Key`.


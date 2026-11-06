# Notifications build plan

Working plan for Notifications (`docs/features/notifications.md`). The owner decided on 2026-11-05 to build it ("all good important points, lets implement"); every choice below was proposed by the planning session of 2026-11-05 and is not yet confirmed. When a task lands, its lasting content moves into `docs/features/notifications.md`, `docs/features/metrics-and-alerting.md`, `docs/design.md` and the README; this file then only tracks status.

This changes the design. `docs/design.md` lists "alert delivery (findings are exposed as Prometheus metrics)" under "Out of scope for the first release", and its Overview table says "the tool delivers no alerts itself". T5 rewrites both (see T5). Read-only towards NATS stays true: the notifier reads nats-lens's own database and sends HTTP requests to the configured channels, nothing else.

Examples use the online shop of `inventory.example.yaml`, the demo's systems (`example-prod`, `example-global`, `example-staging`), `hooks.example.com` for a receiver and `nats-lens.example.com` for the UI.

## Open questions for the owner

Each was answered by the session that built T1 as recommended below: proposed, not yet confirmed by the owner.

1. **Alert delivery in scope, with two channel types.** Proposed: a generic webhook with a documented JSON body, and a Slack-compatible incoming webhook (a `text` message). No e-mail, no paging-service API, no user templates in this version. Recommended: yes; a receiver that needs another shape can transform the documented JSON, and the leftovers keep the rest. Answered so: webhook and Slack-compatible channels only (proposed, not yet confirmed).
2. **A new channel, or a database rebuilt from the archive, starts from a baseline.** Proposed: on its first run for a channel, nats-lens records the findings firing then without sending one message each, and sends one "nats-lens now notifies this channel: 3 findings firing" message with a link to Findings. Recommended: yes; the other choice is a burst of one message per open finding each time a channel is added or a database is lost. Answered so: one summary of what is firing (proposed, not yet confirmed); T2 builds it.
3. **Which severities by default.** Proposed: `min_severity: warning`, so a channel receives warning and critical findings and never info unless it asks. Recommended: yes, as info findings are notices, not trouble (`isTrouble`). Answered so: `min_severity` defaults to `warning` (proposed, not yet confirmed).

## Status

| Task | Status |
|---|---|
| T1 Configuration, message formats and `natslens notify test` | built on `feat/notify-config`, awaiting review |
| T2 Notification state and outbox | built on `feat/notify-outbox`, awaiting review |
| T3 Delivery, retries, metrics and the delivery check | built in isolated T3 checkout, awaiting parent review |
| T4 Acknowledgements in notifications | built in isolated N4 checkout with prerequisite `b7b5131`; awaiting parent fresh read-only review |
| T5 Feature docs, design and audit | planned |

## Rules for every step

- Read-only towards NATS. The notifier opens no NATS connection and sends no NATS request.
- Only `natslens run` notifies. `natslens once` and `natslens checks run` send nothing; `natslens demo` reads no configuration file, so it has no channel and sends nothing. The next `natslens run` after a `once` sends the difference between what each channel was last told and the findings as they stand (T2), not every transition `once` wrote.
- The notifier works from what the check engine stored: `findings`, `finding_transitions`, `mutes` (acceptances included) and, after T4, the acknowledgements of `docs/plans/mobile.md`. It never reruns a check.
- Mutes apply when findings are read, as everywhere: a finding is in a channel's view only while no mute or acceptance covers it at the epoch's time.
- A secret (a webhook URL, a token) is never logged, never stored in the database, never in an error text, and never in the `starting` log line that prints the configuration. Logs name the channel and the URL's host only.
- Time parameters as Unix microseconds and `make_timestamp(?::BIGINT)` wherever DuckDB cannot type them from a column (AGENTS.md); store tests run with a non-UTC `TZ`.
- `docs/design.md`, `docs/features/README.md` and the README are edited only by T5 (see "Order with the other phase 3 plans"); the implementation tasks record their departures here, under "Where Tn departed from this plan".

## What backs each part

| Part | Source | Rule |
|---|---|---|
| Firing | `findings` with `state = 'firing'` at the epoch, not covered by a mute in force at the epoch's time | In the channel's view when its severity, check, category and system pass the channel's filters. A finding that enters the view is sent as `firing`. |
| Changed | `findings.severity` against the severity the channel was last sent (`notify_state`) | A severity that rises is sent as `changed`. One that falls but still passes `min_severity` is sent as `changed`; one that falls below it leaves the view and is sent as `resolved` with the reason "severity now info". |
| Resolved | the finding leaves the view because it resolved (`findings.state = 'resolved'`) | Sent as `resolved`, with `resolved_at` and the last detail. Sent only to a channel that was sent the episode's `firing`. |
| Muted | the finding leaves the view because a mute or acceptance now covers it | Sent as `muted` with who muted it, the reason and the end, so a receiver that keeps state can close its incident. |
| Mute ended | a muted finding that still fires enters the view again | Sent as `firing` with the reason "mute ended" (or "acceptance ended", with the acceptance's `end_reason` when nats-lens ended it). |
| Flapping | `findings.flapping` | When a finding in the view starts flapping, one `flapping` message; while it flaps, its `firing`, `changed` and `resolved` are held back. When it stops flapping, the channel is sent whatever then differs from what it was last sent. |
| Reminder | `notify_state.sent_at` and the channel's `repeat_every` | With `repeat_every` set (off by default, at least 1 hour), a finding still in the view and not acknowledged (T4) is sent again as `reminder` once that long has passed since its last message. |
| Acknowledged | the acknowledgements of `docs/plans/mobile.md` T1 | T4: see below. |
| Link | `notifications.ui_url`, the system ID, the finding ID | `<ui_url>/findings/<finding id, path-escaped>?system=<id>`, the URL the UI's own links use (`web/src/lib/view.ts`). Without `ui_url`, messages carry no link and start logs a warning once. |
| Text | `findings.detail`, the check's name and category from `checks.Set`, the account's readable name from `accounts` | As the Findings page words them: "<check name>: <entity> (<detail>)". |
| Dedup key | system, finding ID, episode start epoch (`findings.since_epoch`), kind, epoch | `example-prod:stream-001:APP/order-jobs:41230:firing:41230`. The same message sent twice carries the same key. |

## T1 Configuration, message formats and `natslens notify test`

No database and no API in this task: what a channel is, what it receives, and a command that proves a channel works.

- **Configuration.** A top-level `notifications` block, shared by every system as `checks` is (`internal/config/notifications.go`, a field on `Config`; refused inside a `systems` entry, as the other shared settings are):

  ```yaml
  notifications:
    ui_url: https://nats-lens.example.com
    channels:
      - name: ops-webhook
        type: webhook            # webhook or slack
        url_file: /var/run/secrets/natslens/ops-webhook-url
        bearer_token_file: /var/run/secrets/natslens/ops-webhook-token
        min_severity: warning    # info, warning or critical
        systems: [example-prod]  # empty: every system
        checks: []               # codes; empty: every check
        exclude_checks: [server-005]
        categories: []           # check categories; empty: every category
        kinds: [firing, changed, resolved, flapping, muted, reminder, acknowledged]
        repeat_every: 0          # 0: no reminders; else at least 1h
        timeout: 10s
        give_up_after: 24h
      - name: ops-chat
        type: slack
        url_env: NATSLENS_OPS_CHAT_URL
        min_severity: critical
  ```

  Exactly one of `url`, `url_file` and `url_env`; at most one of `bearer_token_file` and `bearer_token_env`, for `webhook` only. Names are unique and match `^[a-z0-9][a-z0-9-]{0,62}$`, as system IDs do, since they are metric labels. Unknown keys, an unknown system ID, check code or category, a bad severity or kind, a `repeat_every` under an hour, or a `url` that is not `https` (or `http` on a loopback host, for tests) stop the start. `kinds` defaults to every kind; `acknowledged` is accepted from T1 and does nothing until T4.
- **Why files and variables.** A Slack-compatible URL is itself the secret, and so is a webhook token. `url_file` and `bearer_token_file` are read at every delivery attempt, so a rotated Kubernetes Secret applies without a restart; a plain `url` is allowed for a receiver whose URL holds no secret. A file that cannot be read does not stop the start: start logs a warning naming the channel and the key, and its deliveries fail and count as failures (T3), as an `accounts` entry that cannot connect no longer stops a system (owner's decision of 2026-10-26).
- **Redaction.** `internal/config/redact.go` prints a channel with `url` reduced to its scheme and host and every secret key shown as set or not; a test prints the `starting` line of a configuration with a URL that holds a token and finds no part of the token.
- **Message model.** `internal/notify/message.go`: `Message` (kind, reason, the finding's ID, check code and name, category, severity and previous severity, entity type, key and readable name, detail, `since`, `condition_since`, `transitioned_at`, `resolved_at`, flapping, value, mute or acceptance, link, dedup key) and `Batch` (system ID and name, nats-lens version, sent at, messages). Built from store rows by T2; T1 builds them from literals in tests.
- **Webhook body, version 1.** `POST` with `Content-Type: application/json`, `User-Agent: natslens/<version>`, `Authorization: Bearer <token>` when a token is set, and `Idempotency-Key` set to the batch's key (its first and last outbox IDs, T3). The body is `{"version": 1, "sent_at": …, "system": {"id": "example-prod", "name": …}, "nats_lens_version": …, "notifications": [ … ]}`, each item the message's fields in `snake_case`, times in RFC 3339 UTC, unset values left out, never 0. Documented field by field in the README (T5) with one example per kind; a golden file per kind in `internal/notify/testdata/` pins it.
- **Slack-compatible body.** `{"text": …}` only (no blocks, which not every compatible receiver reads): one line per message, at most 10, "and 4 more: <link to Findings>" beyond. A line: severity word in capitals, check code and name, entity, detail, link in `<url|text>` form. Golden files as for the webhook.
- **No templates in this version.** A template would make the body something nats-lens cannot test, and a template that prints a header or the URL leaks the secret into the message. The documented JSON is the contract; templates go to the leftovers.
- **Sending.** `internal/notify/send.go`: one HTTP client per channel with the channel's `timeout` (default 10 s, at most 60 s), no redirects followed (a redirect is a failure: it could carry the token elsewhere), TLS from the system roots. A `*url.Error` is unwrapped before its text is used anywhere, as its message holds the whole URL. The result is delivered (2xx), retry (5xx, 408, 429 with `Retry-After` read, a network error, a timeout) or refused (any other 4xx, which T3 does not retry).
- **`natslens notify test`.** `cmd/natslens/notify.go`, dispatched from `main.go` beside `run`, `once`, `sql`, `checks` and `demo`: `natslens notify test -config natslens.yaml [-channel ops-webhook] [-system example-prod]` sends one batch with a single message of kind `test` ("nats-lens test message from example-prod") to each channel or the one named, and prints "ops-webhook (hooks.example.com): delivered, HTTP 204" or the failure without the URL. It opens no database and no NATS connection, so it works beside a running `natslens run`. Exit status 1 when any channel failed.
- **Tests.** Config: every refusal above; defaults; a file that cannot be read is a warning, not a refusal; redaction. Formats: golden files for each kind and both types, a message without a link, the Slack cut at 10. Send: an `httptest` server for 204, 500, 429 with `Retry-After`, a redirect, a timeout; captured `slog` output and returned errors never contain the URL's path or query or the token. `notify test`: one good and one failing channel, the exit status, the printed lines.
- **Docs.** `natslens.example.yaml` gains the block, commented; the README is T5's.

### Webhook body, version 1

Built in T1 (`internal/notify/webhook.go`), pinned by `internal/notify/testdata/webhook-*.json`, one file per kind plus `webhook-variants.json` (an acceptance, an ended acceptance, a severity that fell to info) and `webhook-no-link.json`. T5 moves this into the README. A field that is unset is left out; a value of 0 is written. Times are RFC 3339 in UTC, to the second. A change a receiver could trip over (a field removed, renamed or retyped) takes a new `version`; a new field does not.

| Field | Type | Meaning |
|---|---|---|
| `version` | number | `1` |
| `sent_at` | time | When the batch was built |
| `system.id`, `system.name` | string | The system the findings belong to; `name` is left out when it is not set |
| `nats_lens_version` | string | nats-lens's version, as in `User-Agent: natslens/<version>` |
| `notifications[]` | array | The messages, in the order they were queued |
| `.kind` | string | `firing`, `changed`, `resolved`, `flapping`, `muted`, `reminder`, `acknowledged`, `baseline` (the one message a channel gets when nats-lens starts notifying it, about no finding), or `test` from `natslens notify test` |
| `.reason` | string | Why, when the kind alone does not say: `mute ended`, `acceptance ended`, `flapping ended`, `severity now info` (or `severity now warning` for a channel with `min_severity: critical`), `no longer selected by the channel` |
| `.finding_id` | string | `<check code>:<entity key>`, as the API spells it |
| `.check_code`, `.check_name`, `.category` | string | The check |
| `.severity` | string | `info`, `warning` or `critical`, as the finding stands |
| `.previous_severity` | string | The severity the channel was last sent, on `changed` and on a `resolved` with `severity now info` |
| `.entity_type`, `.entity_key` | string | The entity, as in the API |
| `.entity_name` | string | The entity's readable name, when it has one |
| `.detail` | string | The finding's detail |
| `.since` | time | When the episode began |
| `.condition_since` | time | When the condition began, when the check knows |
| `.transitioned_at` | time | The finding's latest transition |
| `.resolved_at` | time | On `resolved` from a resolve |
| `.flapping` | boolean | `true` while the finding flaps; left out otherwise |
| `.value` | number | The check's value, when it reports one |
| `.mute.accepted`, `.mute.by`, `.mute.reason`, `.mute.until`, `.mute.end_reason` | | The mute or acceptance, on `muted` and on a `firing` whose mute ended; `until` is an acceptance's review date; `end_reason` is set when nats-lens ended an acceptance |
| `.firing_count` | number | On `baseline`: the findings in the channel's view when it started, 0 included |
| `.link` | string | `<ui_url>/findings/<finding id, path-escaped>?system=<id>`; the baseline and test messages link the system's Findings page; left out without `ui_url` |
| `.dedup_key` | string | `<system>:<finding id>:<episode start epoch>:<kind>:<epoch>`, the same each time the message is sent; the baseline message's is `<system>:baseline:<epoch>`, the test message's `<system>:test:<Unix microseconds>` |

Headers: `Content-Type: application/json`, `User-Agent: natslens/<version>`, `Authorization: Bearer <token>` with a token, `Idempotency-Key: <batch key>`. A Slack channel's request has neither `Authorization` nor `Idempotency-Key`.

### Where T1 departed from this plan

Each point was decided in the session that built T1: proposed, not yet confirmed.

- **Check codes and categories are refused at the check load, not in `internal/config`.** `internal/config` does not know the check files, as with `checks.disabled`; `notify.CheckFilters` runs after `loadChecks` in `natslens run`, `natslens once` and `natslens notify test`, and stops each of them. A category is known when a loaded check has it.
- **Start warnings only in `natslens run`.** `once` and `notify test` send nothing on a schedule, so only `run` warns of a URL file, token file or variable it cannot read (naming the channel, the key and the file or variable) and of a missing `ui_url`. `notify test` prints the same text as its result.
- **A URL in a file or a variable is checked at each attempt.** The `https` (or loopback `http`, `localhost` in any case) rule applies to a plain `url` at the start and to one read from `url_file` or `url_env` at each attempt; a failure names the key and the file, never the URL.
- **Outcomes.** A URL or token that cannot be read is a `retry` outcome, so T3 counts it as a failure and retries it until `give_up_after`, as the file can be fixed without a restart. A redirect (3xx) is `refused`. `Retry-After` is read on every retried status (5xx, 408, 429), seconds or an HTTP date, held to at most 24 hours (`notify.MaxRetryAfter`) so a huge value cannot overflow; T3 caps it at 10 minutes. `natslens notify test` words a retry outcome as `failed` ("broken (hooks.example.com): failed, HTTP 500").
- **Each sender builds its own transport**, not a copy of `http.DefaultTransport`: proxy from the environment, TLS verified against the system roots and at least 1.2, dial and TLS handshake timeouts. `Sender.Close` closes its idle connections, for T3's shutdown.
- **Error texts are scrubbed as well as unwrapped.** Besides unwrapping a `*url.Error`, the sender removes the URL, its user information, path, query and path segments, and the token from any error text it returns.
- **The Slack text has a header line.** `nats-lens, Production (example-prod):` names the system before the message lines, as a channel can receive several systems. A line reads `CRITICAL firing: stream-001 <check name>: <entity> (<detail>) <link|open>`; what happened follows the severity (`changed from warning`, `resolved, severity now info`, `muted by alice@example.com until 2026-11-05 21:30 UTC (migration tonight)`, `accepted by ...`, `still firing since ...`). The entity is its readable name, else its key. Slack's `&`, `<` and `>` are escaped in every text, so a detail cannot make a link or a mention. Line breaks and tabs in every text taken from a finding, a mute or the configuration become spaces, so a detail from NATS cannot forge a line of its own; each such text is cut to 300 runes ending in "…" (`notify.SlackMaxField`), and the whole text to 30,000 bytes (`notify.SlackMaxText`, under Slack's 40,000 characters), the lines beyond it counted on the "and N more" line. Neither body escapes HTML characters in JSON.
- **`kinds: []` means every kind,** as the other empty lists mean everything; `test` is not selectable and always sent.
- **The test message.** Kind `test`, detail "nats-lens test message from example-prod", a link to the system's Findings page with `ui_url`. Without `-system`, a channel's test names the first system of its `systems`, else the first configured system; `-system` names that system for every channel, whether or not the channel's filter takes it. `notify test` validates the whole file (`Config.Validate`) besides the channels.
- **Model additions.** `Batch` carries `Key` (the `Idempotency-Key`) and `UIURL` (for the Slack text's link to Findings); `Message.Mute` is a `notify.Mute` with `Accepted`, `By`, `Reason`, `Until` and `EndReason`. `config.Channel.Accepts` (severity, system, check, excluded check, category) and `config.Channel.Sends` (kind) hold the filters for T2.
- **No `baseline` kind yet.** The one summary message of open question 2 is T2's: it adds the kind to `notify.Kinds` (not to `config.ChannelKinds`, so `kinds` cannot drop it), golden files for both bodies and a row in the table above.
- **The starting line** shows a channel's `url` as scheme and host, and `url_set`, `url_file_set`, `url_env_set`, `bearer_token_file_set` and `bearer_token_env_set`; the file paths and variable names are not printed there. `Channel` also redacts its `url` in `%v`, `%#v`, JSON and YAML, as `NATS` and `Account` do in the first three. An unknown key in a channel reads "unknown key bogus in notifications channel ops-chat".

## T2 Notification state and outbox

The notifier decides what to send after each check run, by comparing what each channel should know with what it was last told, and queues the messages in the database so that a restart loses none.

- **Why a comparison, not a cursor over transitions.** A mute set or lifted between two epochs is no transition; neither is the end of an acceptance or a severity that falls below a channel's `min_severity`. Comparing the channel's view at the epoch with `notify_state` covers transitions, mutes, filters and flapping with one rule, and it needs no cursor: the engine writes at most one transition per finding per epoch, and the notifier runs after every check run, so it misses no state a check run left.
- **Migration `0035_notifications.sql`** (built as 0035, and `docs/plans/mobile.md` T1 takes 0037: see "Where T2 departed"; if another branch lands a migration first, both take the next free numbers before they merge, as neither will have been applied anywhere):
  - `notify_state`: one row per (channel, finding) a channel was told about: `channel`, `finding_id`, `since_epoch` (the episode), `severity` sent, `state` sent (`firing`, `flapping`), `sent_at`, `epoch`. A row is deleted once its `resolved` or `muted` message is queued.
  - `notify_channels`: one row per channel name with `baselined_at`, so a new channel (or a rebuilt database) is recognised (open question 2).
  - `notify_outbox`: one row per message: `id` (from a sequence), `channel`, `created_at`, `epoch`, `kind`, `finding_id`, `dedup_key`, `message` (JSON, the T1 `Message` without the link, which is built at sending so a changed `ui_url` applies), `state` (`pending`, `delivered`, `refused`, `failed`), `attempts`, `next_attempt_at`, `delivered_at`, `last_status` (HTTP status), `last_error` (stripped of the URL).
  - Comments on every table and column. None is archived: the database is a cache of the archive for what NATS showed, and what was sent to a webhook is not that; after a rebuild a channel starts from a baseline (open question 2). The design text says so (T5).
- **Reconcile.** `internal/notify/reconcile.go` and `internal/store/notify.go`, after each check run that the engine applied (`Runner.check`, beside `ReviewAcceptances`, as a `Runner.Notify` callback so `internal/runner` does not import `internal/notify`). One query reads the epoch's firing findings with the mute in force at the epoch's time (the rule of `ListFindings` for a current range), one reads `notify_state` and `notify_channels`; the channel filters are applied in Go; one transaction inserts the outbox rows and updates `notify_state`. A channel without a `notify_channels` row gets one, its view written to `notify_state`, and the one baseline message; nothing else for it that epoch.
- **Order.** Within a channel, messages of one epoch are queued in the Findings page's order (`api.CompareWorst`), resolves after firings, so a batch reads worst first.
- **Retention.** `Store.SweepNotifications` (in `internal/store/notify.go`, called from `Runner.sweep`, so `retention.go` is not touched): delivered, refused and failed outbox rows older than 7 days or than `retention`, whichever is shorter; `notify_state` rows of findings the sweep removed; `notify_channels` rows of channels no longer configured (logged).
- **Tests.** A finding that fires, changes severity up, down within the channel's range and below it, and resolves, each giving the right message once; a mute set while firing (`muted`), lifted while firing (`firing`, "mute ended"); an acceptance ended by the growth review (its `end_reason`); a finding muted for its whole episode sends nothing, not even its resolve; flapping: one message, then held, then the difference when it stops; reminders at `repeat_every` and not before; filters by system, check, excluded check, category and severity; a new channel and a store without `notify_state` (a rebuild) get the baseline only; the notifier stopped for 10 epochs then run once sends the net difference; a run of 3,000 findings firing at once is reconciled in one transaction (measure it and say how long it took). Store tests under `TZ=America/Los_Angeles`.
- **Docs.** Only this file's departures; T5 moves them.

### Where T2 departed from this plan

Each point was decided in the session that built T2: proposed, not yet confirmed.

- **Migration 0035, not 0036.** The migration runner applies only the files numbered above the database's highest version, so a lower number merged after a higher one is skipped on every database that already has the higher. T2 was built first and took 0035, and `docs/plans/mobile.md` T1 now takes 0037 because T3 delivery takes 0036 (both plans updated; proposed, not yet confirmed).
- **`notify_channels` has `baselined_epoch`, `reconciled_at` and `reconciled_epoch`** besides `baselined_at`. A reconcile of an epoch at or before `reconciled_epoch` changes nothing for the channel (`store.ErrNotifyReconciled` guards the write), so an epoch reconciled twice queues its messages once. `reconciled_at` bounds the "mute ended" search: a `firing` message is given the reason `mute ended` (or `acceptance ended`, with the mute's or acceptance's fields) when a mute that covered the finding ended after the channel's previous reconcile and by this epoch's start, and after the episode began. A finding muted from its first epoch to its last sends nothing, resolve included; one whose mute ended long before it fires again names no mute.
- **Columns named `epoch`.** `notify_state.epoch` and `notify_outbox.epoch` keep the plan's names; `TestSweepCoversEveryEpochTable` lists the notify tables as swept by `SweepNotifications`, and `TestEveryTableArchivedOrExempt` lists them as exempt, with the reason.
- **Mutes at the epoch's start.** A finding's mute is the one in force at the epoch's `started_at` (`Mute.ActiveAt`), as `FindingsAt` reads it, not at the wall clock: a mute created between two epochs applies from the next one, and an acceptance the growth review ends at the epoch's time is already ended in the same epoch's reconcile.
- **Hooks are two functions.** `Runner.Notify(ctx, epoch) error` runs after the acceptance review of an epoch whose checks were applied, also when the review failed; `Runner.NotifySweep(ctx, now) error` runs after a retention sweep that worked. `natslens run` sets `Notify` when `notifications.channels` is not empty and `NotifySweep` always, so that the rows of channels removed from the file go also when none is left (`systemRuntime.notifyHooks`); `natslens once` sets neither. A channel whose `systems` leaves the system out gets no rows in that system's store, not even a baseline.
- **The reconcile's rules for the cases the table leaves open:**
  - Episode changed between two reconciles (the finding resolved and fired again while no reconcile ran): `resolved` with the old episode's dedup key, then `firing` for the new one, kept in that order in the batch.
  - Filter no longer takes a finding the channel was told of (the file changed between two runs): `resolved` with the reason `no longer selected by the channel`.
  - Severity below `min_severity`: the reason is `severity now <severity>`, so `severity now warning` for a channel at `critical`.
  - Flapping ends: one message for the difference: `firing` with the reason `flapping ended` when it fires in the view, else `resolved`, `muted` or `resolved` with `severity now ...`. A flapping finding the channel was told of is sent `muted` at once when a mute covers it.
  - Finding gone from the store while the channel knew it (the notifier did not run for longer than retention): `resolved` built from `notify_state`, with the check's code, name and category and the entity key; the entity type and detail are left out.
  - A kind left out by the channel's `kinds` is not queued, but `notify_state` moves as if it were, so it is not queued again at the next epoch.
  - Reminders: a reminder is due once `repeat_every` has passed since the epoch of the last message about the finding to the channel (`sent_at` is the epoch's start, not the wall clock).
- **Baseline.** Kind `baseline` in `notify.Kinds` (not in `config.ChannelKinds`, so `kinds` cannot leave it out), with `firing_count`, the detail "nats-lens now notifies this channel: 3 findings firing" ("no findings", "1 finding"), and the dedup key `<system>:baseline:<epoch>`; sent also with no finding firing, so a new channel's receiver sees it work. The Slack line is the detail and `<link|Findings>`. Golden files `webhook-baseline.json` and `slack-baseline.json`. The baseline records firing findings in the view as `firing`, or `flapping` when they flap, so a flapping finding sends no flapping message after it.
- **Outbox form of a message.** `notify.Message` has JSON tags (snake case, `omitzero` times) for `notify_outbox.message`; `Message.Encode` drops the link, `notify.Decode` reads it, and `Message.WithLink(ui_url, system)` builds the link at sending (the Findings page for a message about no finding).
- **Outbox IDs.** Taken from `notify_outbox_seq` in one query per reconcile (`nextval` over `range(n)`), sorted and given to the rows in their queued order, so a channel's messages are sent in that order; an ID is never reused after a sweep, which T3's `Idempotency-Key` relies on.
- **Sweep of a removed channel** removes its `notify_channels` and `notify_state` rows and its outbox rows, pending ones included (logged with their count), as no channel is left to send them to. The sweep runs hourly and at the start of `natslens run`, so a channel removed and added again before a sweep keeps its state and gets the net difference, not a baseline; one added again after a sweep gets a baseline. A channel whose `systems` list drops a system is a removed channel for that system's store: its state and pending messages there are deleted.
- **Flapping across a gap.** A flapping finding whose episode also changed while no reconcile ran (it resolved and fired again in the gap) is sent `firing` with the reason `flapping ended` when the flapping stops, without a `resolved` for the old episode: the channel was last told the finding flaps, not that the old episode fires.
- **Mutes read.** A reconcile reads the mutes in force at the epoch's start and those that ended after the oldest channel's `reconciled_at`, not every mute ever made (the table is never swept).
- **Dedup keys unique per channel.** `notify_outbox` has `UNIQUE (channel, dedup_key)`, in 0035 itself, as 0035 has been applied nowhere; a change that would queue a key twice for one channel fails whole.
- **Entity names.** `entity_name` is the key with the account ID replaced by the account's readable name (from `accounts`), for streams and consumers, and the name of an account entity; left out when the account has no other name.
- **Measured.** 3,000 findings firing at once, two channels (4,000 outbox and state rows): the reconcile took about 70 ms, and the 3,000 resolves of the next epoch about 95 ms with the check run applied, on the development laptop (`TestManyFindingsAtOnce`).
- **Tests.** `internal/notify/reconcile_test.go` against a real store: the lifecycle (firing, changed up and down, below the range, back into it, resolved), mutes set and lifted, a check mute that runs out, a mute for the whole episode, an acceptance ended by the review, flapping (one message, held, the difference when it ends, also when it ends resolved), reminders, every filter and kinds, a filter changed across a restart, a channel added later, a store rebuilt from the archive (`rebuild_test.go`), a restart, 10 epochs without a reconcile, the order, two systems, entity names, an epoch reconciled twice, the sweep, a finding lost from the store. `internal/store/notify_test.go` checks the times written and read and the sweep's age boundary. Both pass under `TZ=Asia/Kolkata` and `TZ=America/Los_Angeles`; a time parameter put into arithmetic in the sweep fails `TestSweepNotifications` under `TZ=America/Los_Angeles`.

## T3 Delivery, retries, metrics and the delivery check

- **One sender per system.** `internal/notify/deliver.go`, started for each system by `cmd/natslens/system.go` after its store is open and stopped in its shutdown order (`cmd/natslens/shutdown.go`): it reads the oldest pending rows of each channel, at most 100 per webhook batch or 10 lines per Slack message, sends them as one batch, and marks them in one short write. It never holds the store's lock during a request. One request in flight per channel, so messages arrive in the order they were queued; a failing batch holds the channel's later messages back until it is delivered, refused or given up, so a `resolved` never arrives before its `firing`.
- **Retries.** At least once: a row is marked delivered only after a 2xx, so a crash between the answer and the write sends it again with the same dedup keys and `Idempotency-Key`. Backoff 10 seconds doubling to 10 minutes; `Retry-After` on a 429 or 503 is honoured up to 10 minutes. A refused batch (4xx other than 408 and 429) is marked `refused` at once. A batch still failing `give_up_after` (default 24 h) after its oldest row was queued is marked `failed`, and the channel moves on. Pending rows survive a restart and are sent by the next `natslens run`.
- **Rate.** One request per channel per batch, and a batch is at most one per epoch while nothing fails, so a channel receives about one request per collect interval at most; a burst of findings becomes larger batches (webhook) or a cut line (Slack), never more requests. No separate rate setting in this version.
- **Shutdown.** A request in flight at the signal gets until `shutdown_grace` from the signal and is then cancelled; its rows stay pending. The stop timeout rule of the design ("`shutdown_grace + events.flush_interval + 2s`") does not change.
- **Metrics.** On `/metrics`, per system and `channel` (`internal/api/metrics.go`, `internal/api/storeprov/metrics.go`, read from `notify_outbox` like the other `_last_hour` gauges): `nats_lens_notifications_delivered_last_hour`, `nats_lens_notifications_failed_last_hour` (refused or given up), `nats_lens_notifications_pending`, `nats_lens_notifications_oldest_pending_seconds` (absent with nothing pending). The README (T5) adds an alert rule on the oldest pending age.
- **The delivery check.** `checks/system-00N.sql` "Notifications not delivered", with the next free `system-` code when T3 starts (`system-004` unless the sibling branch `feat/checks-system`, which adds system checks now, has taken it): one finding for the system, warning when a channel has a message pending for more than `pending_for` (threshold, default 10 minutes), critical when a channel refused or gave up a message in the last hour; the detail names the channels, their oldest pending age and their last status, never a URL. It reads `notify_outbox` in the system's own database, so it runs as any check does, also in `natslens checks run`. Its finding goes to the other channels, to the Overview and to `/metrics`; a channel that fails cannot report itself, which is why the metric and the check exist. The check's test in `internal/checks` has a system that fails and one that does not (AGENTS.md).
- **Demo.** None: the demo has no channels. `TestDemoUsesRealChecks` must still pass with the new check (it fires for no demo system); if the fake's remediation list needs the check, that is a one-line change in `internal/api/fake/remediation.go`, which then waits for `docs/plans/mobile.md` T2 to merge (see the order below).
- **Tests.** An `httptest` receiver that fails 3 times then answers 204: one batch, delivered, attempts 4, backoff waits measured on a fake clock; 429 with `Retry-After: 30`; 400 refused at once; give-up after `give_up_after`; order kept with a failing head; a crash simulated between the 2xx and the write sends the same keys again; a request in flight at shutdown stays pending and is sent after a restart; metrics for each state; the check's two entities.
- **Docs.** Departures here; T5 moves them.

## T4 Acknowledgements in notifications

After `docs/plans/mobile.md` T1 has merged (its table and `Finding.acknowledgement`).

- **Acknowledged.** When a finding in a channel's view gets an acknowledgement, the channel is sent `acknowledged` with who acknowledged it, the note and until when it is quiet ("until resolved" when it has no end), so the team sees the note where the alert arrived.
- **Quiet.** While an acknowledgement's quiet is in force: no `reminder`, and a severity that falls is not sent. A severity that rises is still sent, as the situation got worse; a `resolved` is always sent. When the quiet ends and the finding still fires, the channel is sent `reminder` with the reason "acknowledgement quiet ended" whatever `repeat_every` says.
- **Files.** `internal/notify/reconcile.go`, `internal/notify/message.go` (the acknowledgement on the message and in both bodies, golden files updated), `internal/store/notify.go` (the read joins the acknowledgements).
- **Tests.** Each rule above, and an acknowledgement of a finding outside the channel's view sends nothing.

## T5 Feature docs, design and audit

- `docs/features/notifications.md` to `Status: shipped` (or `partial`), with the rules and scenarios as built, `natslens notify test`, and a Not supported list (no templates, no e-mail, no paging-service API, no delivery while nats-lens is down).
- `docs/features/metrics-and-alerting.md`: "nats-lens does not send alerts itself" becomes "besides `/metrics`, nats-lens can send findings to channels"; the new metric families.
- `docs/features/system-checks.md` (the new check), `running-nats-lens.md` (the `notify` command, egress to the receivers: a NetworkPolicy that limits egress must allow them), `demo-mode.md` (no channels in the demo).
- `docs/design.md`, proposed edits: in "Overview", the Alerting row reads "Findings exported at `/metrics`, and sent to configured channels (webhook, Slack-compatible)"; "Out of scope for the first release" drops "alert delivery (findings are exposed as Prometheus metrics)" and adds "notification templates, e-mail and paging-service channels"; a new section "Notifications" under "Checks" (the comparison with `notify_state`, the outbox, at-least-once with dedup keys, the baseline, why it is not archived, secrets read at each attempt and never logged, the delivery check); "Shutdown" gains the in-flight request; the decisions marked proposed, not yet confirmed.
- README: the `notifications.*` rows of the configuration table, the webhook body field by field with an example, `natslens notify test`, the metric rows, and an alert rule `NatsLensNotificationsStuck` on `nats_lens_notifications_oldest_pending_seconds > 600`.
- `docs/features/README.md`: the table row and the coverage line ("[x] Findings sent to webhook and Slack-compatible channels, with mutes, flapping and retries (notifications.md)").
- The audit, `python3 <skill>/scripts/audit_feature_docs.py docs/features/ --map-refs require`; leftovers into `docs/plans/next.md`.

## Order with the other phase 3 plans

Three plans are built together: this one, `docs/plans/data-history.md` (D) and `docs/plans/mobile.md` (M). Tasks in one row can run at the same time; each row starts once the rows above it that it depends on have merged.

| Wave | Run together | Shared files and how they are kept apart |
|---|---|---|
| 1 | D T1, N T1 | D T1: proto, `internal/api`, `storeprov`, `fake`, `apitest`, `internal/archive`, `cmd/natslens/system.go`, `store.go`. N T1: `internal/config`, `internal/notify` (new), `cmd/natslens/main.go`, `notify.go`. No file in common. |
| 2 | D T2, M T1, N T2 | D T2 is web only. M T1: `findings.proto`, `service.proto`, `internal/api`, `storeprov`, `fake`, `apitest`, `internal/store` (acknowledgements, `archive.go`, `retention.go`), migration 0037. N T2: `internal/store/notify.go`, migration 0035, `internal/runner/runner.go`, `cmd/natslens/system.go`, `internal/notify`. M T1 and N T2 share only `internal/store/migrations/` (different files, fixed numbers) and one line each in `migration_golden_test.go`, which the second to merge adds beside the first. |
| 3 | D T3, M T2, N T3 | D T3 is web only. M T2: `oncall.proto`, `service.proto`, `provider.go`, `handler.go`, `storeprov`, `fake`, `apitest`. N T3: `internal/notify`, `internal/api/metrics.go`, `storeprov/metrics.go`, `checks/`, `internal/checks`, `cmd/natslens`. If N T3 needs `fake/remediation.go`, it merges after M T2. N T3 also waits for `feat/checks-system` to merge, or takes the next free code at its own merge. |
| 4 | M T3, N T4, D T4 | M T3 is web only. N T4: `internal/notify`, `internal/store/notify.go`. D T4: docs only. |
| 5 | N T5 | docs |
| 6 | M T4 | docs |

Conflicts that rule out other pairings: every task that changes `service.proto`, `provider.go`, `handler.go`, the fake provider or `apitest/conformance.go` (D T1, M T1, M T2) runs alone among those; `internal/api/metrics.go` is changed by M T1 and N T3, one after the other; `web/src/App.tsx`, `nav.ts`, `requests.ts` and `queries.ts` by D T2 and M T3, and `CauseGroups.tsx` and `Findings.tsx` by D T3 and M T3, in that order; `docs/design.md`, `docs/features/README.md` and the README only by the docs tasks D T4, N T5 and M T4, one at a time. The sibling branches adding checks (`feat/checks-*`) change `checks/`, `internal/checks`, the check docs and the coverage map; N T3 and the docs tasks rebase on them as they land.

## Leftovers

Not in this plan, most valuable first: message templates; a paging-service channel with its own API; e-mail; a channel per finding owner (by account or stream prefix); a Notifications section in the UI listing channels and their last deliveries; quiet hours per channel; grouping by cause card instead of by epoch.

### Where T3 departed from this plan

Proposed, not yet confirmed:

- Migration `0036` remains delivery’s migration; mobile acknowledgements reserve `0037`. Only this isolated, unapplied delivery migration is extended in this review round; applied migrations remain unchanged. It adds durable batch boundaries and `sent_at`, plus a terminal outcome time. A crash after a receiver's 2xx reuses the same batch and key even if later epochs appended messages. A random database incarnation prefixes batch keys: reopening preserves it, while a Parquet rebuild creates a different identity. Existing T2 stores receive an identity during migration without losing pending rows. The identity contains no secrets. Last-hour metrics and the delivery check use outcome time, so a message queued yesterday but refused now counts now.
- Slack delivery selects up to 100 durable rows and uses T1's ten-message cut line with a Findings link. This preserves the plan's burst summary rather than sending ten-row chunks as separate requests.
- Successful and terminal batches wait one collection interval before the channel's next batch, using per-channel watermarks independent of outbox retention across restarts. Terminal rows expire from outcome time, after the shorter of seven days and retention with a minimum one-hour reporting window. Watermarks survive longer collection intervals and sweeps between collections, and are removed when their channel is removed. A successful batch also blocks a new batch until the next stored collection; retries of an existing failing batch remain eligible. Retry waits use persisted next-attempt times; Retry-After can extend, never shorten, exponential backoff. Idle channels poll once per second. Channels have independent workers under one system delivery runner, so a slow channel cannot block healthy channels.
- Delivery starts before NATS connection setup and only for `run`. Its in-flight context is cancelled at the signal's existing `shutdown_grace`; cancellation leaves rows pending, with no new attempts counted. It adds no shutdown allowance.
- The next free delivery check is `system-010`. Its capability doc and the two feature-index sections are updated under the task's explicit exception to T5 file ownership. Global notification and metrics documentation remains T5's.
- `internal/api/fake/remediation.go` already loads remediation dynamically, so no edit or ordering dependency on mobile M T2 was needed. Demo has no outbox and the delivery check does not fire.
- ASVS 1.2.4 / 15.4.3: parameterized short store writes; no store lock across HTTP. ASVS 13.3.1: persisted outcomes and logs omit receiver error text, response bodies and secret destinations; T1 rereads secrets per attempt.

T3 also retains T1's configured-only destinations, verified TLS and redirect refusal (ASVS 13.2.4, 12.3.2), and per-channel timeouts and bounded retries (13.2.6). Persisted failure descriptions are generic (16.5.1); receivers may use long-lived webhook credentials as authorized in T1 rather than the short-lived service identities recommended by 13.2.1. Proposed, not yet confirmed.


### Where T4 departed from this plan

All choices here are proposed, not yet confirmed. N4 merges M T1 `b7b5131` as a separate prerequisite merge; neither review is self-approved.

- The existing notification baseline means last queued, not HTTP-delivered. Acknowledgements use each channel's last reconciled epoch time, not its finding's last message time. Baseline records existing acknowledgements without sending notes. The first subsequent reconcile sends only notes created in `(last reconcile, epoch start]` for the current episode that is in view then. A newly opened episode sends firing before its acknowledgement notes. Resolved or out-of-view episodes do not replay their historical notes.
- Every replacement in that interval sends its own note, even when it was replaced or released during a gap. Acknowledged dedup keys append the acknowledgement ID to the existing key, so multiple notes in one epoch stay distinct. An unchanged acknowledgement is not sent each epoch or on restart. No migration is needed; 0035, 0036, and 0037 are unchanged.
- Quiet is episode-scoped and independent of the severity last queued. A suppressed fall keeps that severity baseline. A replacement that extends quiet suppresses the earlier quiet-ended reminder. Expiry or release gives one quiet-ended reminder if the known finding still fires, regardless of repeat_every. A fall below min_severity stays silent during quiet, then sends the quiet-ended reminder followed by a severity-based resolved message to close the channel's incident. A simultaneous rise sends changed before the reminder. Mute/filter exits retain their normal behavior.
- Acknowledgement notes can arrive while a finding flaps. During quiet a rise still sends changed, and an actual resolve of an acknowledged episode sends resolved even while its flapping flag remains set. Other flapping behavior stays as built in N2.
- The webhook adds `acknowledgement.by`, `.note`, `.quiet_until` or `.until_resolved` to version 1; Slack words and escapes the same fields. The actor comes solely from the existing API-stored acknowledgement. ASVS 1.2.4: bound SQL parameters; 1.2.3: JSON encoding and bounded escaped Slack text; 14.2.1/14.2.3: notes are message-body data only for configured channels; 16.2.5: queue logs contain counts, never actor/note text. No new sender or identity input is introduced.
- Only the minimal notification capability doc and both index sections are updated here as AGENTS.md requires. The global N5 docs pass, M T2 GetOnCall, and phone UI remain deferred to their owners.


### T4 verification evidence

- RED `bf98c79`: `go test ./internal/notify -run TestAcknowledgement -count=1` compiled and failed on missing acknowledged messages, reminders during quiet, downward severity exits, missing quiet-ended reminders, and a flapping resolve. GREEN `8e96662` ran the same target successfully.
- RED `5eef51a`: `TestAcknowledgementNewEpisodeInGap` failed with only resolved/firing and no acknowledgement for the current episode. GREEN `9bdf0d0` preserved that note after the new firing.
- RED `f5a1ac1`: `TestAcknowledgementViewEntryAndThresholdEnd` and `TestAcknowledgementQuietEndWithRise` failed on missing quiet-ended reminder and missing simultaneous changed message. GREEN `4f8f65d` ran the acknowledgement target successfully.
- Final `go test ./internal/notify -count=1 -coverprofile=/tmp/n4-notify-final.out`: PASS, 93.4% statement coverage. Both payload formats have finite-quiet and until-resolved golden fixtures; encode/decode and Slack escaping checks pass.
- `TZ=America/Los_Angeles go test ./internal/notify ./internal/store -run 'Acknowledgement|Ack|Notify' -count=1`: PASS against real stores. Final `TZ=America/Los_Angeles go test -race ./internal/notify -run TestAcknowledgement -count=1`: PASS. `TZ=Asia/Kolkata go test -race ./internal/store -run 'Ack|Notify' -count=1`: PASS. The full notification race suite also passed before the final quiet-end boundary fix.
- Final `go vet ./internal/notify ./internal/store`: PASS. Feature-doc audit with `--map-refs require`: 0 errors, only expected planned-status warnings for data-history and mobile. `git diff --check`: PASS. HTTP tests use local httptest receivers; no configured external destination was contacted.
- Broader `go test ./internal/notify ./internal/store -count=1 -coverprofile=/tmp/n4-coverage.out`: notification package passed; store package reached the aggregate 10-minute timeout while `TestRebuildKeepsTheBaselinedCopyOfAnAcceptance` was running (22 seconds in that test), with no assertion failure reported. The full store suite is not claimed as passed. The test active at timeout passed independently: `go test ./internal/store -run '^TestRebuildKeepsTheBaselinedCopyOfAnAcceptance$' -count=1 -timeout=2m` (10.523 seconds).
- Parent-owned fresh read-only review and integration remain pending. The prerequisite M T1 review is separate; no approval is claimed here.

### T4 first review fix: released episode resolution

Proposed, not yet confirmed. The isolated fix starts at parent `d073334`, which
contains mobile T1 reviewed backend `c45ad0b`. It integrates N4 candidate
`deb34dc` without its older backend merge. Backend integration is satisfied;
parent-owned fresh notification review remains pending, with no approval claimed.

- Review reproduced an acknowledged flapping episode released, reconciled past
  its quiet end, then actually resolved: no resolved message was queued. The
  acknowledgement input excludes releases behind the channel cursor.
- Append `0038_notify_acknowledged.sql` after the parent's highest migration,
  `0037`; `0035`, `0036` and `0037` remain untouched. A boolean on each
  `notify_state` row remembers acknowledgement of that known episode after
  release, expiry, history retention and restart. Existing history/outbox rows
  cannot provide durable memory because they are swept. The marker follows the
  channel's state lifecycle, resets on episode changes, and is not archived.
- Backfill uses only matching episode history created by the channel's cursor.
  New channels, removal/re-addition and rebuild retain normal baseline behavior.
  Only actual resolves of known acknowledged episodes bypass the flapping hold;
  other flapping, mute, filter, gap and severity behavior remains in force.
- ASVS 1.2.4 / 15.4.3: the marker uses the existing parameterized JSON writes and
  locked reconcile transaction; it adds no actor, note, destination or log input.

First-review fix verification:

- RED on integrated candidate: independent `TestReviewAckReleasedEpisodeResolveWhileFlapping`
  and ported `TestAcknowledgementReleasedEpisodeResolveWhileFlapping` both got
  no message instead of resolved. With the fix removed via Go overlay, that
  regression and both `TestAcknowledgementEndedFlappingDiskRestart` variants
  (release and expiry) fail at the missing actual resolve. All pass with the fix.
- `TZ=America/Los_Angeles GOCACHE=/tmp/n4-review-gocache go test -race ./internal/notify -count=1`:
  PASS (30.327s), including delivery, gap, rebuild and payload tests. The sandbox
  initially prohibited httptest loopback listeners; the permitted local-listener
  rerun passed. Final strengthened new-channel assertions also pass in the
  targeted race run of the three new acknowledgement regressions.
- `TZ=America/Los_Angeles GOCACHE=/tmp/n4-review-gocache go test -race ./internal/store -run 'Ack|Notif|TestMigrationsMatchGolden' -count=1 -timeout=10m`:
  PASS (12.768s), including the new real-store upgrade backfill test.
- All five supplied independent acknowledgement probes plus the original
  released-episode probe pass under the corrected checkout-specific overlay,
  non-UTC. `go vet ./internal/notify ./internal/store` passes. Feature-doc audit
  with `--map-refs require`: zero errors, only the two expected planned-status
  warnings. `git diff --check` passes.
- The current parent `develop` at `88eaed8` still ends migrations at `0037`,
  confirming `0038` is the next number. No full store suite was run in this fix
  round, and none is claimed. Backfill can recover only retained history; it
  cannot reconstruct acknowledgements already deleted before upgrading.

### T4 second review fix: channel isolation and upgrade evidence

Proposed, not yet confirmed. This isolated checkout integrates `426e247` onto
`88eaed8`; the feature-index conflict preserves D3's data-history row and N4's
notification row. Reviewed mobile backend `c45ad0b` is an ancestor. No parent
checkout, deployment, receiver, or remote is changed. Fresh third review remains
parent-owned and pending; this implementation does not approve itself.

- The independent second review reproduces two failures: an inactive channel's
  old cursor exposes released history to a channel baselined later, and 0038's
  original backfill grants that same memory from creation time alone.
- Runtime acquisition requires the finding in this channel's view and either an
  acknowledgement in force at the epoch or a note created after this channel's
  previous reconcile. Baseline only takes in-force acknowledgements. Ended notes
  sent during a reconcile gap still count. Quiet expiry does not end the
  acknowledgement. Existing durable memory remains episode-scoped across
  release, expiry, restart and retention; forgetting a state clears its memory.
- The parent ends at migration 0037. Only isolated, unapplied 0038 is revised;
  0035–0037 are unchanged. Upgrade inference no longer treats the reconcile
  cursor as proof of visibility. A baseline proves visibility at its time. For a
  later state observation, a retained firing, changed, reminder or acknowledged
  message must prove visibility of the same channel, finding and episode while
  the acknowledgement was in force. A retained acknowledged or quiet-ended
  message at the saved observation also proves knowledge after release.
- A saved flapping observation alone is insufficient: it can describe a resolve
  held while flapping. Older messages cannot prove uninterrupted visibility
  across filter or mute exits and re-entry. Ambiguous upgrade history stays
  false. If the proving message or acknowledgement was swept before upgrade,
  the migration cannot recover that memory. The runtime marker remains durable
  after upgrade and does not depend on retained outbox/history rows.
- ASVS 1.2.4 / 2.3.1 / 2.3.3: static migration queries and existing bound runtime
  writes preserve per-channel ordering and the atomic reconcile transaction.
  No new receiver, actor, secret, log field, or network operation is introduced.

Second-review verification (all commands run in this isolated checkout):

- RED `f33dcc2`: `TZ=America/Los_Angeles GOCACHE=/tmp/n4-review-gocache go test -overlay=/tmp/n4-second-isolation-current.json ./internal/notify ./internal/store -run TestSecondReview -count=1`
  compiles and fails both independent probes on the integrated candidate. The
  overlay only corrects the supplied overlay's checkout paths. Both tests are
  also ported into the repository. The identical command is GREEN after the fix
  (notify 0.869s, store 0.716s).
- With the integrated candidate's reconcile restored through
  `/tmp/n4-second-reviewed-runtime.json`, the new isolation, baseline release
  boundaries and filter-reentry tests fail; original independent acknowledgement
  probes and the released/restart regressions pass. With `deb34dc`'s reconcile
  restored through `/tmp/n4-second-original-deb.json`, the original independent
  released-episode regression and both restart variants fail on missing resolve.
  These tests all pass with the final implementation.
- RED `5ad3a85`: `TZ=America/Los_Angeles GOCACHE=/tmp/n4-review-gocache go test ./internal/store -run TestNotifyAcknowledgementUpgradeEvidence -count=1`
  fails `active-history-without-view-evidence` before the conservative refinement.
  GREEN uses the full acknowledgement/migration target after `b677853`.
- Final `TZ=America/Los_Angeles GOCACHE=/tmp/n4-review-gocache go test -race -overlay=/tmp/n4-third-original-current.json ./internal/notify -count=1 -coverprofile=/tmp/n4-second-final-coverage.out`:
  PASS (24.757s), 93.9% statement coverage. This includes all original independent
  probes, delivery, restart, sweep, rebuild, dedup, gap, replacement, new-episode,
  channel-baseline and filter tests. The sandbox denied a local httptest listener;
  the rerun with local-listener permission passed without external requests.
- Final `TZ=America/Los_Angeles GOCACHE=/tmp/n4-review-gocache go test -race ./internal/store -run 'Ack|Notif|TestMigrationsMatchGolden' -count=1 -timeout=10m`:
  PASS (12.996s), including populated upgrades, retained-message evidence,
  missing evidence, separate channel/episode keys, repeated migration, and
  inclusive creation/exclusive release at baseline. No full store suite was run
  or is claimed as passed in this round.
- `GOCACHE=/tmp/n4-review-gocache go vet ./internal/notify ./internal/store`:
  PASS. Feature-doc audit with `--map-refs require`: zero errors, only the expected
  planned-mobile warning. `git diff --check`: PASS.

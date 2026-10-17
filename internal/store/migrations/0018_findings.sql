-- Findings: the current state of every check for every entity it failed
-- for, the history of their state changes, the outcome of every check run,
-- and the expected inventory of streams, consumers and buckets.

CREATE TABLE findings (
    id                     VARCHAR PRIMARY KEY,
    check_code             VARCHAR NOT NULL,
    entity_type            VARCHAR NOT NULL,
    entity_key             VARCHAR NOT NULL,
    severity               VARCHAR NOT NULL,
    state                  VARCHAR NOT NULL,
    last_transition        VARCHAR NOT NULL,
    since                  TIMESTAMP NOT NULL,
    since_epoch            BIGINT NOT NULL,
    transitioned_at        TIMESTAMP NOT NULL,
    resolved_at            TIMESTAMP,
    last_seen_epoch        BIGINT NOT NULL,
    updated_epoch          BIGINT NOT NULL,
    detail                 VARCHAR NOT NULL,
    value                  DOUBLE,
    cause_event_id         BIGINT,
    related_config         JSON,
    related_source         VARCHAR,
    transitions_last_hour  INTEGER NOT NULL DEFAULT 0,
    flapping               BOOLEAN NOT NULL DEFAULT false,
    CHECK (state IN ('firing', 'resolved')),
    CHECK (last_transition IN ('firing', 'changed', 'resolved')),
    CHECK (severity IN ('info', 'warning', 'critical'))
);
COMMENT ON TABLE findings IS 'Current state of every (check, entity) pair a check ever failed for: one row per finding, updated after every epoch the checks ran. Mutes are not applied here; they apply when findings are read. Resolved findings are removed by the retention sweep once their last change is older than retention.';
COMMENT ON COLUMN findings.id IS 'Finding ID: check code and entity key joined by a colon, for example consumer-010:APP/order-jobs/resize-worker. The system entity has an empty key (system-001:).';
COMMENT ON COLUMN findings.check_code IS 'Code of the check, for example stream-001.';
COMMENT ON COLUMN findings.entity_type IS 'server, system, stream, consumer or client.';
COMMENT ON COLUMN findings.entity_key IS 'Canonical entity key: server name, account/stream, account/stream/consumer, client connection name; empty for the system.';
COMMENT ON COLUMN findings.severity IS 'Severity of the latest result: info, warning or critical.';
COMMENT ON COLUMN findings.state IS 'firing while the check returns the entity; resolved after the first epoch it does not.';
COMMENT ON COLUMN findings.last_transition IS 'Kind of the latest transition: firing (started), changed (severity changed while firing) or resolved.';
COMMENT ON COLUMN findings.since IS 'Start time of the epoch in which the finding last started firing.';
COMMENT ON COLUMN findings.since_epoch IS 'Epoch in which the finding last started firing.';
COMMENT ON COLUMN findings.transitioned_at IS 'Start time of the epoch of the latest transition.';
COMMENT ON COLUMN findings.resolved_at IS 'Start time of the epoch in which the finding resolved; null while firing.';
COMMENT ON COLUMN findings.last_seen_epoch IS 'Latest epoch in which the check returned the entity.';
COMMENT ON COLUMN findings.updated_epoch IS 'Latest epoch in which the row changed. The archive keeps the copy with the highest value.';
COMMENT ON COLUMN findings.detail IS 'What fails, with the numbers, as the check returned it in last_seen_epoch.';
COMMENT ON COLUMN findings.value IS 'The check''s number for the entity (a ratio, a count, a rate), when it returns one.';
COMMENT ON COLUMN findings.cause_event_id IS 'Event (events.id) that most likely caused the finding, for example the consumer.deleted event of a missing durable; null when none.';
COMMENT ON COLUMN findings.related_config IS 'Configuration settings that bear on the finding: JSON object of setting name to value as text.';
COMMENT ON COLUMN findings.related_source IS 'Where related_config comes from: inventory, or the server name that reported it.';
COMMENT ON COLUMN findings.transitions_last_hour IS 'Transitions of the finding in the hour before updated_epoch.';
COMMENT ON COLUMN findings.flapping IS 'True when transitions_last_hour is above three.';

CREATE TABLE finding_transitions (
    finding_id      VARCHAR NOT NULL,
    check_code      VARCHAR NOT NULL,
    entity_type     VARCHAR NOT NULL,
    entity_key      VARCHAR NOT NULL,
    epoch           BIGINT NOT NULL,
    time            TIMESTAMP NOT NULL,
    kind            VARCHAR NOT NULL,
    severity        VARCHAR NOT NULL,
    detail          VARCHAR NOT NULL,
    value           DOUBLE,
    cause_event_id  BIGINT,
    CHECK (kind IN ('firing', 'changed', 'resolved'))
);
COMMENT ON TABLE finding_transitions IS 'Every change of a finding''s state: at most one row per finding per epoch. Swept and archived with its epoch.';
COMMENT ON COLUMN finding_transitions.finding_id IS 'Finding ID (findings.id).';
COMMENT ON COLUMN finding_transitions.check_code IS 'Code of the check.';
COMMENT ON COLUMN finding_transitions.entity_type IS 'Entity type of the finding.';
COMMENT ON COLUMN finding_transitions.entity_key IS 'Entity key of the finding.';
COMMENT ON COLUMN finding_transitions.epoch IS 'Epoch whose check run made the transition (epochs.epoch).';
COMMENT ON COLUMN finding_transitions.time IS 'Start time of that epoch.';
COMMENT ON COLUMN finding_transitions.kind IS 'firing: the check started returning the entity; changed: its severity changed; resolved: the check stopped returning it.';
COMMENT ON COLUMN finding_transitions.severity IS 'Severity after the transition; for resolved, the severity it had while firing.';
COMMENT ON COLUMN finding_transitions.detail IS 'Detail the check returned at the transition; for resolved, the last detail while firing.';
COMMENT ON COLUMN finding_transitions.value IS 'The check''s number at the transition.';
COMMENT ON COLUMN finding_transitions.cause_event_id IS 'Cause event at the transition (events.id).';

CREATE TABLE check_runs (
    epoch        BIGINT NOT NULL,
    check_code   VARCHAR NOT NULL,
    status       VARCHAR NOT NULL,
    rows         INTEGER NOT NULL,
    duration_ms  BIGINT NOT NULL,
    error        VARCHAR,
    CHECK (status IN ('ok', 'error', 'disabled'))
);
COMMENT ON TABLE check_runs IS 'Outcome of every check after every epoch, so that a check that did not run (error, disabled, or no row at all) is told apart from a check that passed.';
COMMENT ON COLUMN check_runs.epoch IS 'Epoch the check ran for (epochs.epoch).';
COMMENT ON COLUMN check_runs.check_code IS 'Code of the check.';
COMMENT ON COLUMN check_runs.status IS 'ok: ran and its rows were applied; error: the query failed or timed out, and the check''s findings were left as they were; disabled: listed in checks.disabled, not run, and its findings were resolved.';
COMMENT ON COLUMN check_runs.rows IS 'Entities the check returned (failing entities); 0 unless status is ok.';
COMMENT ON COLUMN check_runs.duration_ms IS 'Time the query took, milliseconds.';
COMMENT ON COLUMN check_runs.error IS 'Error text when status is error.';

CREATE TABLE inventory (
    account               VARCHAR NOT NULL,
    kind                  VARCHAR NOT NULL,
    name                  VARCHAR NOT NULL,
    stream                VARCHAR NOT NULL,
    consumer              VARCHAR,
    replicas              INTEGER,
    retention             VARCHAR,
    storage               VARCHAR,
    discard               VARCHAR,
    max_age_ns            BIGINT,
    max_bytes             BIGINT,
    max_msgs              BIGINT,
    max_msgs_per_subject  BIGINT,
    ack_wait_ns           BIGINT,
    max_deliver           INTEGER,
    max_ack_pending       INTEGER,
    filter_subjects       VARCHAR[],
    source                VARCHAR NOT NULL,
    loaded_at             TIMESTAMP NOT NULL,
    CHECK (kind IN ('stream', 'kv', 'object_store', 'consumer')),
    CHECK ((kind = 'consumer') = (consumer IS NOT NULL))
);
COMMENT ON TABLE inventory IS 'Expected streams, KV buckets, object stores and consumers from the inventory_file, replaced on every start. Empty without an inventory file. A null setting is not compared.';
COMMENT ON COLUMN inventory.account IS 'Account name as the inventory file gives it; matched against accounts.account and accounts.account_name.';
COMMENT ON COLUMN inventory.kind IS 'stream, kv, object_store or consumer.';
COMMENT ON COLUMN inventory.name IS 'Name as the inventory gives it: the stream, bucket or consumer name.';
COMMENT ON COLUMN inventory.stream IS 'Stream that holds the asset: the stream itself, KV_<bucket>, OBJ_<bucket>, or for a consumer the stream it reads.';
COMMENT ON COLUMN inventory.consumer IS 'Consumer name for kind consumer; null otherwise.';
COMMENT ON COLUMN inventory.replicas IS 'Expected replica count.';
COMMENT ON COLUMN inventory.retention IS 'Expected retention policy: limits, interest or workqueue.';
COMMENT ON COLUMN inventory.storage IS 'Expected storage: file or memory.';
COMMENT ON COLUMN inventory.discard IS 'Expected discard policy: old or new.';
COMMENT ON COLUMN inventory.max_age_ns IS 'Expected max_age (a bucket''s TTL), nanoseconds; 0 is unlimited.';
COMMENT ON COLUMN inventory.max_bytes IS 'Expected max_bytes; -1 is unlimited.';
COMMENT ON COLUMN inventory.max_msgs IS 'Expected max_msgs; -1 is unlimited.';
COMMENT ON COLUMN inventory.max_msgs_per_subject IS 'Expected max_msgs_per_subject (a KV bucket''s history).';
COMMENT ON COLUMN inventory.ack_wait_ns IS 'Expected ack_wait of a consumer, nanoseconds.';
COMMENT ON COLUMN inventory.max_deliver IS 'Expected max_deliver of a consumer.';
COMMENT ON COLUMN inventory.max_ack_pending IS 'Expected max_ack_pending of a consumer.';
COMMENT ON COLUMN inventory.filter_subjects IS 'Expected subject filters of a consumer, sorted.';
COMMENT ON COLUMN inventory.source IS 'Path of the inventory file the row came from.';
COMMENT ON COLUMN inventory.loaded_at IS 'UTC time the file was loaded.';

-- The replies of micro services ($SRV.INFO and $SRV.STATS), asked over the
-- account connections of the accounts listed under accounts.

CREATE TABLE micro_requests (
    epoch    BIGINT NOT NULL,
    label    VARCHAR NOT NULL,
    account  VARCHAR NOT NULL,
    request  VARCHAR NOT NULL,
    state    VARCHAR NOT NULL,
    replies  INTEGER NOT NULL,
    error    VARCHAR
);
CREATE UNIQUE INDEX micro_requests_key ON micro_requests (epoch, label, request);
COMMENT ON TABLE micro_requests IS 'One row per account and micro service request sent in an epoch: STATS every epoch, INFO every subscriptions_interval, on the first epoch and when STATS names an instance no INFO described. An account listed under accounts with micro: false, or not listed, has no rows: its services were not asked.';
COMMENT ON COLUMN micro_requests.epoch IS 'Epoch that sent the request (epochs.epoch).';
COMMENT ON COLUMN micro_requests.label IS 'The account''s name under accounts in the configuration.';
COMMENT ON COLUMN micro_requests.account IS 'Account as the servers name it (accounts.account; its public key in operator mode), from $SYS.REQ.USER.INFO on the account connection; the label when the user may not ask that or no answer came yet.';
COMMENT ON COLUMN micro_requests.request IS 'INFO ($SRV.INFO) or STATS ($SRV.STATS).';
COMMENT ON COLUMN micro_requests.state IS 'ok: at least one instance replied; no_responders: the server answered that nothing in the account listens, so the account has no micro service; timeout: no reply within the collect window; error: the request could not be made or answered (account connection not connected, a permissions violation on the request subject or the reply inbox, or only replies that did not decode).';
COMMENT ON COLUMN micro_requests.replies IS 'Replies that decoded: one per instance.';
COMMENT ON COLUMN micro_requests.error IS 'Why the state is error, as the connection or server said it, or how many replies did not decode; NULL otherwise. A request the server refused is not sent again for 10 minutes: in between its row repeats the refusal with "(not sent: refused at <time>, asked again after <time>)" appended.';

CREATE TABLE micro_instances (
    epoch        BIGINT NOT NULL,
    account      VARCHAR NOT NULL,
    service      VARCHAR NOT NULL,
    instance_id  VARCHAR NOT NULL,
    version      VARCHAR,
    description  VARCHAR,
    metadata     JSON,
    endpoints    JSON NOT NULL
);
COMMENT ON TABLE micro_instances IS 'One row per service instance that answered $SRV.INFO in an epoch that sent it (micro_requests, request INFO); an epoch that did not send INFO has no rows. Each instance is the newest INFO row of its (account, service, instance_id) at or before an epoch.';
COMMENT ON COLUMN micro_instances.epoch IS 'Epoch that sent INFO (epochs.epoch).';
COMMENT ON COLUMN micro_instances.account IS 'Account of the service, as micro_requests.account.';
COMMENT ON COLUMN micro_instances.service IS 'Service name; one service per (account, service).';
COMMENT ON COLUMN micro_instances.instance_id IS 'Instance ID the framework gave the instance when it started.';
COMMENT ON COLUMN micro_instances.version IS 'Service version the instance reported; NULL when it reported none.';
COMMENT ON COLUMN micro_instances.description IS 'Service description the instance reported; NULL when it reported none.';
COMMENT ON COLUMN micro_instances.metadata IS 'Service metadata the instance reported, a JSON object of strings; NULL when it reported none.';
COMMENT ON COLUMN micro_instances.endpoints IS 'Endpoints the instance reported, a JSON array of objects with name, subject, queue_group and metadata, as $SRV.INFO gives them; [] for an instance without endpoints.';

CREATE TABLE micro_stats (
    epoch        BIGINT NOT NULL,
    account      VARCHAR NOT NULL,
    service      VARCHAR NOT NULL,
    instance_id  VARCHAR NOT NULL,
    version      VARCHAR,
    started      TIMESTAMP
);
COMMENT ON TABLE micro_stats IS 'One row per service instance that answered $SRV.STATS in an epoch, endpoints or not; its endpoints are in micro_endpoints. An instance of the last epochs with no row here did not answer.';
COMMENT ON COLUMN micro_stats.epoch IS 'Epoch that sent STATS (epochs.epoch).';
COMMENT ON COLUMN micro_stats.account IS 'Account of the service, as micro_requests.account.';
COMMENT ON COLUMN micro_stats.service IS 'Service name.';
COMMENT ON COLUMN micro_stats.instance_id IS 'Instance ID.';
COMMENT ON COLUMN micro_stats.version IS 'Service version the instance reported with its stats; NULL when it reported none.';
COMMENT ON COLUMN micro_stats.started IS 'When the instance started, in UTC by its own clock: its counters in micro_endpoints count from here, and a later value is a new run whose counters started again from 0. NULL when it reported none.';

CREATE TABLE micro_endpoints (
    epoch                       BIGINT NOT NULL,
    account                     VARCHAR NOT NULL,
    service                     VARCHAR NOT NULL,
    instance_id                 VARCHAR NOT NULL,
    endpoint                    VARCHAR NOT NULL,
    subject                     VARCHAR,
    queue_group                 VARCHAR,
    num_requests                BIGINT NOT NULL,
    num_errors                  BIGINT NOT NULL,
    processing_time_ns          BIGINT NOT NULL,
    average_processing_time_ns  BIGINT NOT NULL,
    last_error                  VARCHAR,
    data                        JSON
);
COMMENT ON TABLE micro_endpoints IS 'One row per instance and endpoint in each $SRV.STATS reply of an epoch (micro_stats). The counters are cumulative from the instance''s start (micro_stats.started).';
COMMENT ON COLUMN micro_endpoints.epoch IS 'Epoch that sent STATS (epochs.epoch).';
COMMENT ON COLUMN micro_endpoints.account IS 'Account of the service, as micro_requests.account.';
COMMENT ON COLUMN micro_endpoints.service IS 'Service name.';
COMMENT ON COLUMN micro_endpoints.instance_id IS 'Instance ID (micro_stats.instance_id).';
COMMENT ON COLUMN micro_endpoints.endpoint IS 'Endpoint name.';
COMMENT ON COLUMN micro_endpoints.subject IS 'Subject the endpoint listens on, as STATS reported it, else from the instance''s newest INFO; NULL when neither gave one.';
COMMENT ON COLUMN micro_endpoints.queue_group IS 'Queue group of the endpoint''s subscription, as STATS reported it, else from the newest INFO; NULL for an endpoint without one.';
COMMENT ON COLUMN micro_endpoints.num_requests IS 'Requests the endpoint received since the instance started.';
COMMENT ON COLUMN micro_endpoints.num_errors IS 'Requests the endpoint answered with an error since the instance started.';
COMMENT ON COLUMN micro_endpoints.processing_time_ns IS 'Time the endpoint spent on its requests since the instance started, in nanoseconds.';
COMMENT ON COLUMN micro_endpoints.average_processing_time_ns IS 'processing_time_ns divided by num_requests, as the instance reported it, in nanoseconds; 0 before the first request.';
COMMENT ON COLUMN micro_endpoints.last_error IS 'Last error the endpoint returned; NULL when it has returned none.';
COMMENT ON COLUMN micro_endpoints.data IS 'Custom stats the service attached to the endpoint, as JSON, up to 1 KB; more is stored as {"truncated": true, "bytes": <size>}. NULL when it attached none.';

CREATE TABLE micro_endpoint_hours (
    hour                TIMESTAMP NOT NULL,
    account             VARCHAR NOT NULL,
    service             VARCHAR NOT NULL,
    endpoint            VARCHAR NOT NULL,
    requests            BIGINT NOT NULL,
    errors              BIGINT NOT NULL,
    processing_time_ns  BIGINT NOT NULL,
    epochs              INTEGER NOT NULL
);
CREATE UNIQUE INDEX micro_endpoint_hours_key ON micro_endpoint_hours (hour, account, service, endpoint);
COMMENT ON TABLE micro_endpoint_hours IS 'Per hour, what each endpoint of a service did, summed over its instances: the increase of the micro_endpoints counters between each STATS row and the instance''s previous row within the hour before, added to the hour of the later row''s epoch as each epoch is written. A counter that fell or a new micro_stats.started is a new run, whose increase is its value. An instance''s first row counts its value when the instance started after the previous epoch, else nothing. Swept by hour with the oldest kept epoch and archived by hour.';
COMMENT ON COLUMN micro_endpoint_hours.hour IS 'Start of the hour, in UTC, of the epochs whose increases are summed (epochs.started_at truncated to the hour).';
COMMENT ON COLUMN micro_endpoint_hours.account IS 'Account of the service, as micro_requests.account.';
COMMENT ON COLUMN micro_endpoint_hours.service IS 'Service name.';
COMMENT ON COLUMN micro_endpoint_hours.endpoint IS 'Endpoint name. Each STATS row is paired with the instance''s previous row of the same name and subject, or of the same name when either has no subject; two endpoints of one name with different subjects (in two groups) are summed into one row.';
COMMENT ON COLUMN micro_endpoint_hours.requests IS 'Requests the endpoint''s instances received in the hour.';
COMMENT ON COLUMN micro_endpoint_hours.errors IS 'Requests the endpoint''s instances answered with an error in the hour.';
COMMENT ON COLUMN micro_endpoint_hours.processing_time_ns IS 'Time the endpoint''s instances spent on requests in the hour, in nanoseconds; divided by requests it is the hour''s average time.';
COMMENT ON COLUMN micro_endpoint_hours.epochs IS 'Epochs in the hour whose STATS the endpoint answered.';

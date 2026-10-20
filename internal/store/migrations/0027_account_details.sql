-- What an ACCOUNTZ lookup reads about one account: the name, the system
-- flag, imports and exports, and the account JWT's claims (issue time,
-- expiry, limits). The JWT itself is never stored.

CREATE TABLE account_details (
    epoch                BIGINT NOT NULL,
    server_id            VARCHAR NOT NULL,
    account              VARCHAR NOT NULL,
    looked_up_at         TIMESTAMP NOT NULL,
    account_name         VARCHAR,
    is_system            BOOLEAN NOT NULL,
    expired              BOOLEAN NOT NULL,
    imports              INTEGER NOT NULL,
    exports              INTEGER NOT NULL,
    has_jwt              BOOLEAN NOT NULL,
    jwt_issued_at        TIMESTAMP,
    jwt_expires          TIMESTAMP,
    limit_connections    BIGINT,
    limit_leafnodes      BIGINT,
    limit_subscriptions  BIGINT,
    limit_data           BIGINT,
    limit_payload        BIGINT,
    limit_js_disk        BIGINT,
    limit_js_memory      BIGINT,
    limit_js_streams     BIGINT,
    limit_js_consumers   BIGINT
);
CREATE UNIQUE INDEX account_details_key ON account_details (epoch, account);
COMMENT ON TABLE account_details IS 'One row per answered ACCOUNTZ lookup of an account. Each account is looked up at most every 10 minutes and at most 32 accounts per epoch, so an epoch holds rows only for the accounts looked up in it: read the newest row at or before an epoch. Values come from the JWT''s claims; the JWT itself is not stored.';
COMMENT ON COLUMN account_details.epoch IS 'Epoch in which the lookup was made (epochs.epoch).';
COMMENT ON COLUMN account_details.server_id IS 'Server whose reply was read: the first that answered with the account (servers.server_id).';
COMMENT ON COLUMN account_details.account IS 'Account identifier (accounts.account).';
COMMENT ON COLUMN account_details.looked_up_at IS 'UTC time the reply was read.';
COMMENT ON COLUMN account_details.account_name IS 'Readable name: the JWT name tag, else the name in the JWT; in config mode the account identifier. NULL when none is known.';
COMMENT ON COLUMN account_details.is_system IS 'True for the system account.';
COMMENT ON COLUMN account_details.expired IS 'True when the server reports the account JWT as expired.';
COMMENT ON COLUMN account_details.imports IS 'Imports of the account, without the service imports from the system account that nats-server adds to every account.';
COMMENT ON COLUMN account_details.exports IS 'Exports of the account. The system account counts the exports nats-server adds to it.';
COMMENT ON COLUMN account_details.has_jwt IS 'True when the reply carried a decoded account JWT (operator mode). Without one the JWT and limit columns are NULL.';
COMMENT ON COLUMN account_details.jwt_issued_at IS 'UTC time the account JWT was issued (iat); NULL without a JWT or when it does not say.';
COMMENT ON COLUMN account_details.jwt_expires IS 'UTC time the account JWT expires (exp); NULL without a JWT or when it never expires.';
COMMENT ON COLUMN account_details.limit_connections IS 'Most client connections the account JWT allows; -1 no limit; NULL without a JWT.';
COMMENT ON COLUMN account_details.limit_leafnodes IS 'Most leaf node connections the account JWT allows; -1 no limit; NULL without a JWT.';
COMMENT ON COLUMN account_details.limit_subscriptions IS 'Most subscriptions the account JWT allows; -1 no limit; NULL without a JWT.';
COMMENT ON COLUMN account_details.limit_data IS 'Most bytes the account JWT allows in flight; -1 no limit; NULL without a JWT.';
COMMENT ON COLUMN account_details.limit_payload IS 'Largest message payload the account JWT allows, bytes; -1 no limit; NULL without a JWT.';
COMMENT ON COLUMN account_details.limit_js_disk IS 'JetStream file storage the account JWT allows, bytes; -1 no limit, 0 JetStream not enabled; with tiered limits the sum over the tiers, -1 when any tier has no limit; NULL without a JWT.';
COMMENT ON COLUMN account_details.limit_js_memory IS 'JetStream memory storage the account JWT allows, bytes; -1 no limit, 0 JetStream not enabled; tiered limits summed as for limit_js_disk; NULL without a JWT.';
COMMENT ON COLUMN account_details.limit_js_streams IS 'Most streams the account JWT allows; -1 no limit; tiered limits summed as for limit_js_disk; NULL without a JWT.';
COMMENT ON COLUMN account_details.limit_js_consumers IS 'Most consumers the account JWT allows; -1 no limit; tiered limits summed as for limit_js_disk; NULL without a JWT.';

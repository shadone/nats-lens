-- The servers' subscription lists (SUBSZ), collected every
-- subscriptions_interval with an epoch rather than in every epoch.

CREATE TABLE subscriptions (
    epoch      BIGINT NOT NULL,
    server_id  VARCHAR NOT NULL,
    account    VARCHAR NOT NULL,
    subject    VARCHAR NOT NULL,
    queue      VARCHAR,
    cid        BIGINT NOT NULL,
    subs       INTEGER NOT NULL,
    msgs       BIGINT NOT NULL
);
COMMENT ON TABLE subscriptions IS 'Subscriptions each server holds for its own connections at an epoch that collected subscription lists (SUBSZ), up to subsz_limit per server; an epoch without a collection has no rows (see subscription_lists). One row per (epoch, server_id, account, subject, queue, cid): subscriptions of one connection with the same subject and queue are folded. A server lists its clients'' subscriptions and those of its own system, JetStream and account clients, not the interest its routes, gateways and leaf nodes carry, so a client subscription appears once, on the server the client is connected to.';
COMMENT ON COLUMN subscriptions.epoch IS 'Epoch that collected the list (epochs.epoch).';
COMMENT ON COLUMN subscriptions.server_id IS 'Server that holds the subscriptions (servers.server_id).';
COMMENT ON COLUMN subscriptions.account IS 'Account of the subscribing connection (accounts.account). The global account is $G, as in account_stats, where connections.account is empty: join connections on (epoch, server_id, cid) only.';
COMMENT ON COLUMN subscriptions.subject IS 'Subscribed subject, wildcards included.';
COMMENT ON COLUMN subscriptions.queue IS 'Queue group; NULL for a subscription without one.';
COMMENT ON COLUMN subscriptions.cid IS 'Connection ID on that server: joins connections.cid of the same epoch and server_id for a client. An ID CONNZ does not list is one of the server''s own clients, a client CONNZ left out at connz_limit or that connected or left between the two requests, or any connection when CONNZ did not answer that server in that epoch.';
COMMENT ON COLUMN subscriptions.subs IS 'Subscriptions folded into this row: the connection''s subscriptions with this subject and queue.';
COMMENT ON COLUMN subscriptions.msgs IS 'Messages delivered to these subscriptions since they were made, summed.';

CREATE TABLE subscription_lists (
    epoch      BIGINT NOT NULL,
    server_id  VARCHAR NOT NULL,
    total      BIGINT NOT NULL,
    listed     BIGINT NOT NULL
);
CREATE UNIQUE INDEX subscription_lists_key ON subscription_lists (epoch, server_id);
COMMENT ON TABLE subscription_lists IS 'One row per server that answered SUBSZ in an epoch that collected subscription lists: an epoch with a row here has its subscriptions rows for that server, possibly none. Whether SUBSZ was asked and how it went is in epoch_endpoints (endpoint SUBSZ).';
COMMENT ON COLUMN subscription_lists.epoch IS 'Epoch that collected the list (epochs.epoch).';
COMMENT ON COLUMN subscription_lists.server_id IS 'Server that answered (servers.server_id).';
COMMENT ON COLUMN subscription_lists.total IS 'Subscriptions the server listed before the limit (SUBSZ total): its clients'' and its own, not route, gateway or leaf node interest. Less than server_stats.subscriptions, which counts those too.';
COMMENT ON COLUMN subscription_lists.listed IS 'Subscriptions in the reply, at most subsz_limit, before folding into subscriptions rows. Below total when the list was cut at the limit.';

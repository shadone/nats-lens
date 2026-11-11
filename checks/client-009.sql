/*
code: client-009
name: Duplicate subscription
entity: client
category: clients
severity: warning
kind: operational
lookback: 30m
description: >-
  A connection of this application holds more than one plain subscription (without a queue group) on the same
  subject, in each server's newest subscription list within the lookback (the servers' subscription lists, read
  every subscriptions_interval). The client receives every message on the subject once per subscription. It is
  usually a handler registered twice, for example on every reconnect or in a loop. Left out: subscriptions in a
  queue group, as several queue subscriptions on one connection are the usual way to handle messages
  concurrently in the client libraries; _INBOX. subjects, as a request library may hold several reply
  subscriptions on one inbox pattern; $SRV. subjects, which each micro service on a shared connection
  subscribes to; and the servers' own subscriptions: a subscription counts only when its connection is in the
  server's connection list of the same collection. The detail names the newest of the lists read. Grouped by
  application, the connection name before the first "@", as the Applications page groups it; a client without a
  name is (unnamed). The finding resolves once each server's newest list within the lookback shows no such subscription,
  or no list lies within it.
remediation: >-
  Find where the application subscribes to the subject named in the detail and make it subscribe once: a
  subscription made in a reconnect handler or a loop is the usual cause, as the client library restores its
  subscriptions on reconnect by itself. `nats server request subscriptions --subscriptions --filter-account
  <account>` lists each connection's subscriptions to confirm the count. If several handlers on one subject are
  meant to share its messages, subscribe them in a queue group.
related_config: [lookback]
value_name: connections with a duplicate subscription
value_basis: >-
  connections of the application, in each server's newest subscription list within the lookback, that hold a
  plain subscription on one subject more than once
*/
-- Each server's newest list within the lookback, so a server that missed
-- the newest collection keeps its duplicates.
WITH coll AS (
    SELECT server_id, epoch FROM subscription_lists
    WHERE epoch >= getvariable('window_epoch') AND epoch <= getvariable('epoch')
    QUALIFY row_number() OVER (PARTITION BY server_id ORDER BY epoch DESC) = 1
),
d AS (
    SELECT coalesce(nullif(split_part(c.name, '@', 1), ''), '(unnamed)') AS app, s.server_id, sv.server_name, s.cid,
        s.subject, s.queue, s.subs, s.epoch
    FROM subscriptions s JOIN coll ON s.epoch = coll.epoch AND s.server_id = coll.server_id
    JOIN connections c ON c.epoch = s.epoch AND c.server_id = s.server_id AND c.cid = s.cid
    JOIN servers sv ON sv.server_id = s.server_id
    WHERE s.subs > 1 AND s.queue IS NULL AND NOT starts_with(s.subject, '_INBOX.') AND NOT starts_with(s.subject, '$SRV.')
      AND NOT tooling_at(c.name, c.ip, c.epoch)
),
-- Each subject of an application once: how often it is held and by how
-- many connections.
items AS (
    SELECT app, subject, count(*) AS conns,
        subject || ' '
            || CASE WHEN min(subs) = max(subs) THEN '' ELSE 'up to ' END || max(subs) || ' times on ' || count(*)
            || CASE WHEN count(*) = 1 THEN ' connection' ELSE ' connections' END AS text
    FROM d GROUP BY app, subject
),
per AS (
    SELECT app, list(text ORDER BY conns DESC, subject) AS texts
    FROM items GROUP BY app
),
agg AS (
    SELECT app, max(epoch) AS epoch, count(DISTINCT (server_id, cid)) AS n,
        list(DISTINCT server_name ORDER BY server_name) AS servers
    FROM d GROUP BY app
)
SELECT
    a.app AS entity_key,
    a.n || CASE WHEN a.n = 1 THEN ' connection holds' ELSE ' connections hold' END
        || ' a subscription more than once in the subscription lists of ' || strftime(e.started_at, '%H:%M:%S') || ' UTC: '
        || array_to_string(p.texts[1:5], '; ')
        || CASE WHEN len(p.texts) > 5 THEN '; and ' || (len(p.texts) - 5) || ' more' ELSE '' END
        || ' (' || array_to_string(a.servers[1:5], ', ')
        || CASE WHEN len(a.servers) > 5 THEN ' and ' || (len(a.servers) - 5) || ' more' ELSE '' END || ').' AS detail,
    a.n AS value
FROM agg a JOIN per p USING (app) JOIN epochs e ON e.epoch = a.epoch

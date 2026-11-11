/*
code: client-009
name: Duplicate subscription
entity: client
category: clients
severity: warning
kind: operational
lookback: 30m
description: >-
  A connection of this application holds more than one subscription with the same subject and queue group, in the
  newest subscription collection within the lookback (the servers' subscription lists, read every
  subscriptions_interval). Without a queue group the client receives every message on the subject once per
  subscription; in a queue group the server counts each subscription as a member, so the connection takes a
  larger share of the group's messages than its peers. It is usually a handler registered twice, for example on
  every reconnect or in a loop. Subscriptions on _INBOX. subjects are left out, as a request library may hold
  several reply subscriptions on one inbox pattern, and so are the servers' own subscriptions: a subscription
  counts only when its connection is in the server's connection list of the same collection. Grouped by
  application, the connection name before the first "@", as the Applications page groups it; a client without a
  name is (unnamed). The finding resolves once a collection within the lookback shows no such subscription, or no
  collection lies within it.
remediation: >-
  Find where the application subscribes to the subject named in the detail and make it subscribe once: a
  subscription made in a reconnect handler or a loop is the usual cause, as the client library restores its
  subscriptions on reconnect by itself. `nats server request subscriptions --subscriptions --filter-account
  <account>` lists each connection's subscriptions to confirm the count. If two subscriptions on one subject are
  intended, give them different queue groups or subjects.
related_config: [lookback]
value_name: connections with a duplicate subscription
value_basis: >-
  connections of the application in the newest subscription collection within the lookback that hold a
  subscription with the same subject and queue group more than once
*/
WITH coll AS (
    SELECT max(epoch) AS epoch FROM subscription_lists
    WHERE epoch >= getvariable('window_epoch') AND epoch <= getvariable('epoch')
),
d AS (
    SELECT coalesce(nullif(split_part(c.name, '@', 1), ''), '(unnamed)') AS app, s.server_id, sv.server_name, s.cid,
        s.subject, s.queue, s.subs, s.epoch
    FROM subscriptions s JOIN coll ON s.epoch = coll.epoch
    JOIN connections c ON c.epoch = s.epoch AND c.server_id = s.server_id AND c.cid = s.cid
    JOIN servers sv ON sv.server_id = s.server_id
    WHERE s.subs > 1 AND NOT starts_with(s.subject, '_INBOX.') AND NOT tooling_at(c.name, c.ip, c.epoch)
),
-- Each subject and queue group of an application once: how often it is
-- held and by how many connections.
items AS (
    SELECT app, subject, queue, count(*) AS conns,
        subject || coalesce(' in queue group ' || queue, '') || ' '
            || CASE WHEN min(subs) = max(subs) THEN '' ELSE 'up to ' END || max(subs) || ' times on ' || count(*)
            || CASE WHEN count(*) = 1 THEN ' connection' ELSE ' connections' END AS text
    FROM d GROUP BY app, subject, queue
),
per AS (
    SELECT app, list(text ORDER BY conns DESC, subject, queue NULLS FIRST) AS texts
    FROM items GROUP BY app
),
agg AS (
    SELECT app, any_value(epoch) AS epoch, count(DISTINCT (server_id, cid)) AS n,
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

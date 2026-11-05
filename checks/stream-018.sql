/*
code: stream-018
name: Single replica in a cluster
entity: stream
category: storage
severity: info
kind: operational
thresholds:
  - name: min_servers
    default: "3"
    unit: count
    description: >-
      JetStream servers the stream could be placed on, in its cluster and with its placement tags, before one replica
      is reported.
description: >-
  A stream, KV bucket or object store has one replica while min_servers or more JetStream servers could hold its
  replicas, so losing that one server's disk loses its messages, and while that server is down the stream is
  unavailable. The servers counted are those the collection expects (those that answered, those that stopped
  answering within server_forget_after, and the route and JetStream meta peers the answering servers report), in the
  stream's cluster and domain, that its placement tags allow as nats-server matches them: without case, a tag with a
  leading ! rules out servers that carry it, and a server tagged !jetstream takes no stream. A mirror is reported
  too; its detail says that its origin holds the data. Not reported: an asset whose replicas the inventory gives
  (one replica there is by design; another number is stream-012's), and a stream on a server without a cluster.
  While the stream's only server is not ready, its last configuration is kept, so a restart does not resolve and
  fire the finding again.
remediation: >-
  If the data matters, raise the replicas: `nats stream edit <stream> --replicas 3`, for a KV bucket
  `nats kv edit <bucket> --replicas 3`, for an object store `nats object edit <bucket> --replicas 3`, or change the
  definition the asset comes from (a NACK resource, or the code that creates the bucket) so a later deploy does not
  set it back. The new replicas copy the stream from the current one; for a large stream do it outside busy hours and
  watch stream-003. A stream pinned by placement tags needs that many servers with the tags. If one replica is by
  design, for a cache or data that is rebuilt on start, declare `replicas: 1` for it in the inventory, or mute the
  finding with that reason.
related_config: [min_servers, inventory_file]
value_name: JetStream servers it could be placed on
value_basis: >-
  JetStream servers the collection expects in the stream's cluster and domain that could hold a replica: they carry
  every required placement tag of the stream and none of its "!" tags (compared without case), and are not tagged
  "!jetstream"
*/
WITH nr AS (
    SELECT server_name FROM not_ready(getvariable('epoch'))
),
-- The current configuration, or while the stream's only reporting server
-- is not ready, the configuration it reported last.
cfg AS (
    SELECT c.account, c.stream, c.replicas, coalesce(c.mirror, '') AS mirror, coalesce(c.placement_tags, []) AS tags,
        s.server_name, s.cluster, coalesce(s.domain, '') AS domain
    FROM stream_config c JOIN servers s USING (server_id)
    WHERE c.first_epoch <= getvariable('epoch')
      AND (c.last_epoch >= getvariable('epoch')
           OR (s.server_name IN (SELECT server_name FROM nr)
               AND c.last_epoch = (SELECT st.last_epoch FROM streams st WHERE st.account = c.account AND st.stream = c.stream)))
    QUALIFY row_number() OVER (PARTITION BY c.account, c.stream ORDER BY c.last_epoch DESC, s.server_name) = 1
),
-- The servers the collection expects, each by its newest process, with
-- their tags in lower case as nats-server compares them; a server tagged
-- !jetstream takes no stream.
newest AS (
    SELECT server_name, jetstream, cluster, coalesce(domain, '') AS domain,
        list_transform(coalesce(tags, []), x -> lower(trim(x))) AS tags
    FROM servers
    WHERE list_contains((SELECT expected_names FROM epochs WHERE epoch = getvariable('epoch')), server_name)
    QUALIFY row_number() OVER (PARTITION BY server_name ORDER BY last_epoch DESC, server_id DESC) = 1
),
members AS (
    SELECT * FROM newest WHERE jetstream AND coalesce(cluster, '') <> '' AND NOT list_contains(tags, '!jetstream')
),
-- Each placement tag of a stream, and whether it excludes.
stags AS (
    SELECT account, stream, lower(trim(CASE WHEN starts_with(t, '!') THEN t[2:] ELSE t END)) AS tag,
        starts_with(t, '!') AS exclude
    FROM (SELECT account, stream, trim(unnest(tags)) AS t FROM cfg)
),
-- A server can hold the stream when no tag rules it out: a tag it lacks,
-- or an excluding tag it carries.
sized AS (
    SELECT c.*, (SELECT count(*) FROM members z WHERE z.cluster = c.cluster AND z.domain = c.domain
                 AND NOT EXISTS (SELECT 1 FROM stags s WHERE s.account = c.account AND s.stream = c.stream
                                 AND list_contains(z.tags, s.tag) = s.exclude)) AS servers
    FROM cfg c WHERE c.replicas = 1 AND coalesce(c.cluster, '') <> ''
),
-- Assets whose replicas the inventory gives; the inventory names the
-- account as the servers do or by its readable name.
declared AS (
    SELECT coalesce((SELECT a.account FROM accounts a WHERE i.account IN (a.account, a.account_name)
                     ORDER BY a.account = i.account DESC LIMIT 1), i.account) AS account, i.stream
    FROM inventory i
    WHERE i.kind IN ('stream', 'kv', 'object_store') AND i.replicas IS NOT NULL
)
SELECT
    account || '/' || stream AS entity_key,
    CASE WHEN stream LIKE 'KV\_%' ESCAPE '\' THEN 'KV bucket ' || stream[4:]
         WHEN stream LIKE 'OBJ\_%' ESCAPE '\' THEN 'Object store ' || stream[5:]
         ELSE 'Stream ' || stream END
        || ' has 1 replica, on ' || server_name || ', in cluster ' || cluster || ' of ' || servers
        || ' JetStream servers' || CASE WHEN len(tags) > 0 THEN ' with tags ' || array_to_string(tags, ', ') ELSE '' END
        || CASE WHEN mirror <> '' THEN '; it mirrors ' || mirror || ', which holds the data'
                WHEN stream LIKE 'OBJ\_%' ESCAPE '\' THEN '; its objects are lost with that server''s disk'
                ELSE '; its messages are lost with that server''s disk' END
        || CASE WHEN server_name IN (SELECT server_name FROM nr) THEN ' (that server is not ready now)' ELSE '' END
        || '.' AS detail,
    servers AS value,
    json_object('replicas', CAST(replicas AS VARCHAR), 'placement_tags', array_to_string(tags, ', ')) AS related_config,
    server_name AS related_source
FROM sized
WHERE servers >= getvariable('min_servers')
  AND (account, stream) NOT IN (SELECT account, stream FROM declared)

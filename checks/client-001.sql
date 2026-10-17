/*
code: client-001
name: High client round trip time
entity: client
category: clients
severity: warning
kind: operational
thresholds:
  - name: max_rtt
    default: 100ms
    unit: duration
    description: Round trip time above which a connection counts.
  - name: min_epochs
    default: "2"
    unit: count
    description: Consecutive epochs a connection of the service must be above max_rtt before the finding fires.
description: >-
  A connection of this service has had a round trip time to its server above max_rtt in each of the last min_epochs
  epochs (the same client: server, pod (the name after "@") and address, or server and cid without a pod), so
  pods that are slow while they start do not fire and a pod that reconnects while slow does. A pod whose address
  the server sees changing between epochs (a NAT or proxy in between) counts as a new client, which breaks the run.
  Connections are grouped by the service,
  the connection name before any "@" (name each pod's connection service@pod to get this), so a mute covers every
  pod of it; a client without a name is grouped as (unnamed).
remediation: >-
  Inside one Kubernetes cluster the round trip should be a millisecond or two. A high value for a pod in the same
  cluster means the client is busy (CPU throttled, blocked event loop in a Node.js service) and answers pings late;
  look at the pod's CPU. A client that reaches NATS through a load balancer adds a few milliseconds; much more
  points at that path or at a client whose CPU is throttled between requests, so check the client's CPU allocation
  before the network. A client outside the cluster (a developer laptop, another region) is expected to be slower:
  mute the finding for that client with a reason.
related_config: [max_rtt, min_epochs]
value_name: worst round trip time, ms
value_basis: largest RTT among the client's connections in the epoch
*/
WITH last AS (
    SELECT epoch FROM epochs WHERE epoch <= getvariable('epoch') ORDER BY epoch DESC
    LIMIT CAST(getvariable('min_epochs') AS INTEGER)
),
c AS (
    SELECT c.epoch, coalesce(nullif(split_part(c.name, '@', 1), ''), '(unnamed)') AS client, c.server_id, s.server_name, c.cid,
        -- The same client across epochs: the pod (after "@") and its
        -- address on its server, which survive a reconnect; the cid when the
        -- name gives no pod.
        CASE WHEN nullif(split_part(coalesce(c.name, ''), '@', 2), '') IS NOT NULL
             THEN 'pod ' || split_part(c.name, '@', 2) || ' ' || coalesce(c.ip, '')
             ELSE 'cid ' || c.cid END AS instance,
        c.rtt_ns, c.ip
    FROM connections c JOIN servers s USING (server_id)
    WHERE c.epoch IN (SELECT epoch FROM last) AND c.rtt_ns IS NOT NULL AND NOT tooling_at(c.name, c.ip, c.epoch)
),
-- The same client (server and pod, else server and cid) must be slow in
-- every one of the last min_epochs epochs: two pods each slow once while
-- starting do not add up, a pod that reconnects while slow does.
slow_conns AS (
    SELECT client, server_id, instance, count(DISTINCT epoch) AS n FROM c
    WHERE rtt_ns > epoch(getvariable('max_rtt')) * 1e9 GROUP BY client, server_id, instance
),
slow_epochs AS (
    SELECT client, max(n) AS n FROM slow_conns GROUP BY client
),
agg AS (
    SELECT client, count(*) AS total,
        count(*) FILTER (WHERE rtt_ns > epoch(getvariable('max_rtt')) * 1e9) AS slow,
        max(rtt_ns) AS worst, arg_max(server_name || ' cid ' || cid || coalesce(' from ' || ip, ''), rtt_ns) AS worst_at
    FROM c WHERE epoch = getvariable('epoch') GROUP BY client
)
SELECT
    a.client AS entity_key,
    a.slow || ' of ' || a.total || CASE WHEN a.total = 1 THEN ' connection' ELSE ' connections' END || ' above '
        || ns_text(epoch(getvariable('max_rtt')) * 1e9) || '; worst ' || round(a.worst / 1e6, 1) || 'ms on ' || a.worst_at || '.' AS detail,
    round(a.worst / 1e6, 1) AS value
FROM agg a JOIN slow_epochs s USING (client)
WHERE a.slow > 0 AND s.n >= getvariable('min_epochs')

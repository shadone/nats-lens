/*
code: consumer-016
name: Single replica on a replicated stream
entity: consumer
category: health
severity: info
kind: operational
acceptable: true
description: >-
  A durable consumer is configured with one replica (num_replicas 1) while its stream has more. The consumer's
  state, its delivered position and acknowledgements, is kept on one server only: while that server restarts the
  consumer delivers nothing, although its stream keeps its quorum, and if the server loses its disk the durable
  loses its position. Only streams with limits retention are judged: nats-server runs a consumer of a work-queue
  or interest stream at the stream's replica count whatever its configuration says. A consumer with replicas 0
  takes the stream's count and is not reported. Ephemeral and
  ordered consumers (one replica by design) and internal JS_ consumers of mirrors and sources are left out.
remediation: >-
  If the durable must survive a server restart without a pause, give it the stream's replica count: `nats consumer
  edit <stream> <consumer> --replicas <stream replicas>` (or 0 to follow the stream). A durable kept at one replica
  on purpose, for throughput, can be muted with that reason. The detail names the server that holds it.
related_config: [replicas]
value_name: stream replicas
value_basis: the stream's configured replica count; the consumer has one
*/
-- nats-server gives a consumer of a work-queue or interest stream the
-- stream's replica count whatever its configuration says, so only limits
-- streams can hold a durable at one replica.
WITH scfg AS (
    SELECT account, stream, max(replicas) AS r FROM stream_config
    WHERE first_epoch <= getvariable('epoch') AND last_epoch >= getvariable('epoch')
    GROUP BY account, stream
    HAVING bool_and(retention = 'limits')
),
-- Every current configuration of the consumer says one replica; servers
-- that disagree are consumer-008's.
ccfg AS (
    SELECT account, stream, consumer
    FROM consumer_config
    WHERE first_epoch <= getvariable('epoch') AND last_epoch >= getvariable('epoch')
    GROUP BY account, stream, consumer
    HAVING min(replicas) = 1 AND max(replicas) = 1
       AND bool_and(coalesce(durable_name, '') <> '')
),
holders AS (
    SELECT c.account, c.stream, c.consumer,
        array_to_string(list(DISTINCT s.server_name ORDER BY s.server_name), ', ') AS servers,
        arg_max(s.server_name, c.is_leader) AS leader
    FROM consumer_stats c JOIN servers s USING (server_id)
    WHERE c.epoch = getvariable('epoch')
    GROUP BY c.account, c.stream, c.consumer
)
SELECT
    k.account || '/' || k.stream || '/' || k.consumer AS entity_key,
    'Durable ' || k.consumer || ' has 1 replica while its stream ' || k.stream || ' has ' || s.r
        || '; its state is on ' || h.servers || ' only.' AS detail,
    s.r AS value,
    json_object('replicas', '1', 'stream_replicas', CAST(s.r AS VARCHAR)) AS related_config,
    h.leader AS related_source
FROM ccfg k
JOIN scfg s USING (account, stream)
JOIN holders h USING (account, stream, consumer)
WHERE s.r > 1 AND k.consumer NOT LIKE 'JS\_%' ESCAPE '\'

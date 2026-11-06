/*
code: server-024
name: OCSP peer rejected
entity: server
category: health
severity: warning
kind: operational
lookback: 15m
description: >-
  This server rejected a TLS peer connection because its certificate chain did not pass OCSP verification.
  Only the final peer connection rejection event counts, not a chain-link status error, an authentication
  error, or an ordinary disconnect. One rejection is enough: that peer could not connect. The event does not
  prove revocation: an unavailable responder or another verification failure can also reject a connection.
  The check needs live system event capture and cannot recover events published while disconnected.
  It keeps the name captured with the event, even after a restart or when the server stops reporting; an
  event without a server name uses its server ID. It resolves when no rejection remains in the lookback.
remediation: >-
  Read the event's reason and peer kind, then the emitting server's TLS and OCSP logs at the event time.
  Use the certificate fingerprint in the raw event to identify the peer in your certificate inventory.
  The peer is the leaf certificate; another certificate in its trust chain may have failed verification.
  Check the chain's revocation status, certificate validity, OCSP responder reachability and the clocks on
  the peer and server. Replace a revoked or invalid certificate through the normal certificate issuance
  process; restore responder access when the certificate is valid. Verify that the intended peer can
  reconnect. Keep certificate and OCSP verification enabled. nats-lens only reads these events.
related_config: [lookback]
value_name: rejected peer connections
value_basis: distinct OCSP peer connection rejection event IDs within the lookback on this server
*/
-- Both predicates bound the scan to the live window before JSON extraction;
-- delayed old events and events newer than this epoch cannot raise a finding.
WITH recent AS (
    SELECT id, time, received_at, server_name,
        json_extract_string(raw, '$.id') AS event_id,
        json_extract_string(raw, '$.server.id') AS server_id
    FROM events
    WHERE source = 'sys' AND kind = 'server.ocsp_peer_rejected'
      AND type = 'io.nats.server.advisory.v1.ocsp_peer_reject'
      AND time > getvariable('window_start') AND time <= getvariable('epoch_time')
      AND received_at > getvariable('window_start') AND received_at <= getvariable('epoch_time')
), unique_events AS (
    SELECT *, coalesce(nullif(server_name, ''), nullif(server_id, '')) AS name
    FROM recent
    QUALIFY row_number() OVER (
        PARTITION BY coalesce(nullif(server_id, ''), server_name), coalesce(nullif(event_id, ''), 'row:' || id)
        ORDER BY received_at DESC, id DESC) = 1
)
SELECT name AS entity_key,
    name || ': ' || count(*) || ' OCSP peer connection rejection' || CASE WHEN count(*) = 1 THEN '' ELSE 's' END
      || ' in the last ' || getvariable('lookback_text') || '; the last at '
      || strftime(max(time), '%Y-%m-%d %H:%M:%S') || ' UTC. Read the server TLS and OCSP logs for the failed chain.' AS detail,
    count(*) AS value,
    arg_max(id, (received_at, id)) AS cause_event_id
FROM unique_events
WHERE name IS NOT NULL
GROUP BY name

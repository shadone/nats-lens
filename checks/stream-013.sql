/*
code: stream-013
name: Stream configuration changed
entity: stream
category: change
severity: info
kind: operational
lookback: 1h
description: >-
  A client updated the stream's configuration within the lookback, from the stream.updated advisories, which
  name the client that made the call.
remediation: >-
  Check that the change was intended: the detail names the client and its address. Where NACK applies the
  declared streams, a change by another client is undone or repeated on NACK's next reconcile; elsewhere a change
  belongs in the stream's definition. Compare with the inventory (stream-012) and between
  the servers (stream-007).
related_config: [lookback]
value_name: configuration updates
value_basis: stream updated advisories within the lookback
*/
WITH ev AS (
    SELECT e.*,
        coalesce((SELECT a.account FROM accounts a WHERE e.account IN (a.account, a.account_name)
                  ORDER BY (a.account = e.account) DESC LIMIT 1), e.account) AS acct
    FROM events e
    WHERE e.kind = 'stream.updated' AND e.received_at > getvariable('window_start') AND e.received_at <= getvariable('epoch_time')
      AND e.stream IS NOT NULL AND e.account IS NOT NULL
)
SELECT
    acct || '/' || stream AS entity_key,
    'Updated ' || count(*) || CASE WHEN count(*) = 1 THEN ' time' ELSE ' times' END || ' in the last '
        || getvariable('lookback_text') || ', the last at ' || strftime(max(time), '%H:%M:%S') || ' UTC by '
        || coalesce(arg_max(client_name, time), 'an unnamed client') || coalesce(' (' || arg_max(client_host, time) || ')', '')
        || '.' AS detail,
    count(*) AS value,
    arg_max(id, time) AS cause_event_id
FROM ev
GROUP BY acct, stream

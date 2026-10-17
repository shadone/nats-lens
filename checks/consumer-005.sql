/*
code: consumer-005
name: Messages reached max_deliver
entity: consumer
category: consumers
severity: warning
kind: operational
lookback: 1h
description: >-
  The consumer sent max deliveries advisories within the lookback: messages were delivered max_deliver times
  without an acknowledgement and the server stopped delivering them. For a work queue the job behind each such
  message is not done.
remediation: >-
  Each advisory names the stream sequence of the message (Events page, consumer.max_deliveries). Read it with
  `nats stream get <stream> <seq>` and find in the worker's log why it failed. Fix the cause, then republish the
  work if it is still needed; the message itself stays in the stream but this consumer will not deliver it again.
related_config: [max_deliver, lookback]
value_name: messages that reached max_deliver
value_basis: max deliveries advisories within the lookback
*/
WITH ev AS (
    SELECT e.*,
        coalesce((SELECT a.account FROM accounts a WHERE e.account IN (a.account, a.account_name)
                  ORDER BY (a.account = e.account) DESC LIMIT 1), e.account) AS acct
    FROM events e
    WHERE e.kind = 'consumer.max_deliveries' AND e.received_at > getvariable('window_start') AND e.received_at <= getvariable('epoch_time')
      AND e.stream IS NOT NULL AND e.consumer IS NOT NULL AND e.account IS NOT NULL
)
SELECT
    acct || '/' || stream || '/' || consumer AS entity_key,
    count(*) || CASE WHEN count(*) = 1 THEN ' message' ELSE ' messages' END || ' reached max_deliver in the last '
        || getvariable('lookback_text') || ', the last at ' || strftime(max(time), '%H:%M:%S') || ' UTC.' AS detail,
    count(*) AS value,
    arg_max(id, time) AS cause_event_id
FROM ev
GROUP BY acct, stream, consumer

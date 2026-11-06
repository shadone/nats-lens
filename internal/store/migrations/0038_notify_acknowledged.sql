-- Channel-local acknowledgement memory survives quiet ending and history
-- retention. It follows notify_state's episode and is not archived.
ALTER TABLE notify_state ADD COLUMN acknowledged BOOLEAN DEFAULT false;
COMMENT ON COLUMN notify_state.acknowledged IS 'The channel knows this episode has been acknowledged; actual resolution bypasses the flapping hold even after quiet ends. Removed with the state, reset for a different episode, and never archived.';

-- Recover only channel-local evidence. The reconcile cursor alone does not
-- prove that the finding was in view when a historical note was active.
-- A baseline proves visibility. Later sent_at alone does not: a flapping
-- message can describe a held resolve. Require a retained firing, changed,
-- reminder or acknowledged message for that observation as well as a note in
-- force then, even if its quiet had expired. An ended note needs the retained acknowledged
-- or quiet-ended message at that observation, with a matching episode key.
-- Older messages cannot prove uninterrupted visibility (filters/mutes can have
-- removed and reopened the same episode), so ambiguous history stays false.
UPDATE notify_state AS st SET acknowledged = true
WHERE EXISTS (
    SELECT 1 FROM notify_channels AS c
    WHERE c.channel = st.channel
      AND st.sent_at >= c.baselined_at AND st.sent_at <= c.reconciled_at
      AND st.epoch >= c.baselined_epoch AND st.epoch <= c.reconciled_epoch
      AND (
          EXISTS (
              SELECT 1 FROM acknowledgements AS a
              WHERE a.finding_id = st.finding_id AND a.since_epoch = st.since_epoch
                AND a.created_at <= st.sent_at
                AND (a.ended_at IS NULL OR a.ended_at > st.sent_at)
                AND (
                    (st.epoch = c.baselined_epoch AND st.sent_at = c.baselined_at)
                    OR EXISTS (
                        SELECT 1 FROM notify_outbox AS seen
                        WHERE seen.channel = st.channel AND seen.finding_id = st.finding_id
                          AND seen.epoch = st.epoch AND seen.created_at = st.sent_at
                          AND seen.kind IN ('firing', 'changed', 'reminder', 'acknowledged')
                          AND regexp_matches(seen.dedup_key,
                              ':' || CAST(st.since_epoch AS VARCHAR) || ':' || seen.kind || ':[0-9]+(:[0-9]+)?$')
                    )
                )
          )
          OR EXISTS (
              SELECT 1 FROM notify_outbox AS o
              WHERE o.channel = st.channel AND o.finding_id = st.finding_id
                AND o.epoch = st.epoch AND o.created_at = st.sent_at
                AND (
                    (o.kind = 'acknowledged' AND regexp_matches(o.dedup_key,
                        ':' || CAST(st.since_epoch AS VARCHAR) || ':acknowledged:[0-9]+:[0-9]+$'))
                    OR (o.kind = 'reminder'
                        AND json_extract_string(o.message, '$.reason') = 'acknowledgement quiet ended'
                        AND regexp_matches(o.dedup_key,
                            ':' || CAST(st.since_epoch AS VARCHAR) || ':reminder:[0-9]+$'))
                )
          )
      )
);

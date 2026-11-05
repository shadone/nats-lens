-- Channel-local acknowledgement memory survives quiet ending and history
-- retention. It follows notify_state's episode and is not archived.
ALTER TABLE notify_state ADD COLUMN acknowledged BOOLEAN DEFAULT false;
COMMENT ON COLUMN notify_state.acknowledged IS 'The channel knows this episode has been acknowledged; actual resolution bypasses the flapping hold even after quiet ends. Removed with the state, reset for a different episode, and never archived.';

-- Recover known episodes when upgrading an existing notifier. Only history
-- already visible at the channel cursor may contribute, never future notes.
UPDATE notify_state AS st SET acknowledged = true
WHERE EXISTS (
    SELECT 1 FROM acknowledgements AS a, notify_channels AS c
    WHERE a.finding_id = st.finding_id AND a.since_epoch = st.since_epoch
      AND c.channel = st.channel AND a.created_at <= c.reconciled_at
);

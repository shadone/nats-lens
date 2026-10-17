-- A finding flaps when it fired and resolved more than three times in the
-- hour, as the design says: the flag counts cycles, not every transition.

COMMENT ON COLUMN findings.transitions_last_hour IS 'Transitions (firing, changed, resolved) of the finding in the hour before updated_epoch.';
COMMENT ON COLUMN findings.flapping IS 'True when the finding resolved more than three times in the hour before updated_epoch: it fired and resolved over and over.';

-- When a finding's condition began, where the check knows it, apart from
-- since (when nats-lens first saw it).

ALTER TABLE findings ADD COLUMN condition_since TIMESTAMP;

COMMENT ON COLUMN findings.condition_since IS 'When the condition began, as the check gives it (a creation time, a process start); NULL when the check does not know. since is when nats-lens first saw the finding.';

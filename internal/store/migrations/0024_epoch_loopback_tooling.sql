-- The loopback guard's decision per epoch, stored by the check engine so
-- that an epoch is judged the same way whenever it is read.

ALTER TABLE epochs ADD COLUMN loopback_tooling BOOLEAN;

COMMENT ON COLUMN epochs.loopback_tooling IS 'Whether clients connected from 127.0.0.1 or ::1 counted as tooling in this epoch (the loopback guard, before checks.loopback_is_tooling applies); NULL for epochs the checks never decided, read as true.';

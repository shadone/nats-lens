-- Connection limits and timeouts from VARZ, to show a server's settings and
-- mark those that differ between servers.

ALTER TABLE server_stats ADD COLUMN max_payload BIGINT;
ALTER TABLE server_stats ADD COLUMN max_pending BIGINT;
ALTER TABLE server_stats ADD COLUMN write_deadline_ns BIGINT;
ALTER TABLE server_stats ADD COLUMN ping_interval_ns BIGINT;
ALTER TABLE server_stats ADD COLUMN max_pings_out INTEGER;

COMMENT ON COLUMN server_stats.max_payload IS 'Largest message payload the server accepts, bytes (VARZ max_payload); NULL without VARZ.';
COMMENT ON COLUMN server_stats.max_pending IS 'Largest amount of unsent data a connection may have before the server closes it as a slow consumer, bytes (VARZ max_pending); NULL without VARZ.';
COMMENT ON COLUMN server_stats.write_deadline_ns IS 'Time a write to a client socket may take before the server closes it as a slow consumer, nanoseconds (VARZ write_deadline); NULL without VARZ.';
COMMENT ON COLUMN server_stats.ping_interval_ns IS 'Interval between the server''s PINGs on an idle connection, nanoseconds (VARZ ping_interval); NULL without VARZ.';
COMMENT ON COLUMN server_stats.max_pings_out IS 'Unanswered PINGs after which the server closes a connection as stale (VARZ ping_max); NULL without VARZ.';

-- What raft_stats.wal_error can and cannot say.

COMMENT ON COLUMN raft_stats.wal_error IS 'Write-ahead log error as RAFTZ encodes it (raw JSON). nats-server 2.15 serialises the Go error value, so the message is lost: a plain error arrives as {}, a file error as {"Op":"write","Path":"...","Err":{}}. Non-null means the group hit a critical write error and was shut down (state CLOSED); the server log has the text.';

-- nats-server sets is_spoke on the server that opened the connection to a
-- hub, so a hub lists the leaf nodes that connected to it with is_spoke
-- false; the comment of 0002 said the opposite.

COMMENT ON COLUMN leafnodes.is_spoke IS 'True when the reporting server is the spoke: it opened this connection to a hub (its own uplink). A hub lists the leaf nodes that connected to it with false.';

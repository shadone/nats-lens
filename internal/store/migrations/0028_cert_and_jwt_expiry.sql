-- When the servers' listener certificates and the clients' user JWTs
-- expire. Both tables are archived; the new columns are nullable, so a
-- file written before this migration loads with NULL in them.

ALTER TABLE server_stats ADD COLUMN tls_cert_not_after TIMESTAMP;
ALTER TABLE server_stats ADD COLUMN cluster_tls_cert_not_after TIMESTAMP;
ALTER TABLE server_stats ADD COLUMN gateway_tls_cert_not_after TIMESTAMP;
ALTER TABLE server_stats ADD COLUMN leafnode_tls_cert_not_after TIMESTAMP;
ALTER TABLE server_stats ADD COLUMN websocket_tls_cert_not_after TIMESTAMP;
ALTER TABLE server_stats ADD COLUMN mqtt_tls_cert_not_after TIMESTAMP;

COMMENT ON COLUMN server_stats.tls_cert_not_after IS 'UTC expiry of the client listener''s TLS certificate (VARZ tls_cert_not_after); NULL when the listener has no TLS, without VARZ, or from a server version that does not report it.';
COMMENT ON COLUMN server_stats.cluster_tls_cert_not_after IS 'UTC expiry of the cluster (route) listener''s TLS certificate (VARZ cluster.tls_cert_not_after); NULL as for tls_cert_not_after.';
COMMENT ON COLUMN server_stats.gateway_tls_cert_not_after IS 'UTC expiry of the gateway listener''s TLS certificate (VARZ gateway.tls_cert_not_after); NULL as for tls_cert_not_after.';
COMMENT ON COLUMN server_stats.leafnode_tls_cert_not_after IS 'UTC expiry of the leaf node listener''s TLS certificate (VARZ leaf.tls_cert_not_after); NULL as for tls_cert_not_after.';
COMMENT ON COLUMN server_stats.websocket_tls_cert_not_after IS 'UTC expiry of the websocket listener''s TLS certificate (VARZ websocket.tls_cert_not_after); NULL as for tls_cert_not_after.';
COMMENT ON COLUMN server_stats.mqtt_tls_cert_not_after IS 'UTC expiry of the MQTT listener''s TLS certificate (VARZ mqtt.tls_cert_not_after); NULL as for tls_cert_not_after.';

ALTER TABLE connections ADD COLUMN user_jwt BOOLEAN;
ALTER TABLE connections ADD COLUMN jwt_expires TIMESTAMP;

COMMENT ON COLUMN connections.user_jwt IS 'True when CONNZ reported a user JWT for the connection. False without one; a bearer JWT is reported empty by the server and so reads false. NULL for rows written before nats-lens recorded it. The JWT itself is not stored.';
COMMENT ON COLUMN connections.jwt_expires IS 'UTC time the user JWT expires (exp, read from its claims without verifying the signature); NULL when it never expires, did not decode, or there is none.';

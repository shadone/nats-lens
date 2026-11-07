-- Corrected comments for columns of 0027 and 0028: which JetStream limits
-- are read and how tiers are combined, the data limit the server does not
-- enforce, and when a listener certificate's expiry is not reported.

COMMENT ON COLUMN account_details.limit_data IS 'The data limit the account JWT states, bytes; -1 no limit; NULL without a JWT. nats-server does not enforce it for an account.';
COMMENT ON COLUMN account_details.limit_js_disk IS 'JetStream file storage the account JWT allows, bytes, comparable with account_stats.js_storage (replicated bytes); -1 no limit, 0 none allowed; NULL without a JWT. The account-wide limit when the JWT sets account-wide disk or memory, as nats-server enforces it; else the tiers: each R<n> tier''s limit times n, summed, -1 when any tier has no limit; a tier key other than R<n> is left out.';
COMMENT ON COLUMN account_details.limit_js_memory IS 'JetStream memory storage the account JWT allows, bytes, comparable with account_stats.js_memory; -1 no limit, 0 none allowed; read from the account-wide limit or the tiers as for limit_js_disk; NULL without a JWT.';
COMMENT ON COLUMN account_details.limit_js_streams IS 'Most streams the account JWT allows; -1 no limit, also when the JWT says 0, which nats-server does not enforce; from the tiers the sum, -1 when any tier has no limit; NULL without a JWT.';
COMMENT ON COLUMN account_details.limit_js_consumers IS 'Most consumers the account JWT allows on each stream (nats-server applies it per stream); -1 no limit, also when the JWT says 0; from the tiers the largest, -1 when any tier has no limit; NULL without a JWT.';

COMMENT ON COLUMN server_stats.tls_cert_not_after IS 'UTC expiry of the client listener''s TLS certificate (VARZ tls_cert_not_after; the first configured certificate); NULL when the listener has no TLS, when its certificate comes from a certificate store or a GetCertificate callback, without VARZ, or from a server version that does not report it.';
COMMENT ON COLUMN server_stats.cluster_tls_cert_not_after IS 'UTC expiry of the cluster (route) listener''s TLS certificate (VARZ cluster.tls_cert_not_after); NULL as for tls_cert_not_after.';
COMMENT ON COLUMN server_stats.gateway_tls_cert_not_after IS 'UTC expiry of the gateway listener''s TLS certificate (VARZ gateway.tls_cert_not_after); NULL as for tls_cert_not_after.';
COMMENT ON COLUMN server_stats.leafnode_tls_cert_not_after IS 'UTC expiry of the leaf node listener''s TLS certificate (VARZ leaf.tls_cert_not_after); NULL as for tls_cert_not_after.';
COMMENT ON COLUMN server_stats.websocket_tls_cert_not_after IS 'UTC expiry of the websocket listener''s TLS certificate (VARZ websocket.tls_cert_not_after); NULL as for tls_cert_not_after.';
COMMENT ON COLUMN server_stats.mqtt_tls_cert_not_after IS 'UTC expiry of the MQTT listener''s TLS certificate (VARZ mqtt.tls_cert_not_after); NULL as for tls_cert_not_after.';

/*
code: server-016
name: TLS certificate expires soon
entity: server
category: health
severity: warning
severity_rule: critical once a certificate expired, or when one expires within crit_within
kind: operational
thresholds:
  - name: warn_within
    default: 14d
    unit: duration
    description: How soon after the collection a certificate's expiry fires the finding.
  - name: crit_within
    default: 3d
    unit: duration
    description: How soon an expiry is critical.
description: >-
  A TLS certificate of one of the server's listeners (client, cluster, gateway, leaf node, websocket, MQTT) expires
  within warn_within of the collection, or has expired. One finding per server; the detail names every listener
  whose certificate expires within warn_within, soonest first. Once a certificate expires, clients, routes,
  gateways or leaf nodes that verify it can no longer connect through that listener. The certificates are read from
  VARZ: from the collection, or, for a server that did not answer VARZ in it, from its newest collection within the
  last 24 hours, as on the Accounts page, so a server that is briefly missing keeps its finding. A listener without
  TLS, or whose certificate comes from a certificate store, reports no expiry and is not reported.
remediation: >-
  Renew the certificate and have the server load it: with cert-manager, check the Certificate resource (`kubectl
  describe certificate -n <namespace>`) and why it did not renew; a renewed secret is read on a configuration reload
  (`nats-server --signal reload`, or the config reloader sidecar of the Helm chart) or a restart. Renewing a
  cluster, gateway or leaf node certificate on one server at a time keeps the routes up.
related_config: [tls_cert_not_after, cluster.tls_cert_not_after, gateway.tls_cert_not_after, leaf.tls_cert_not_after,
  websocket.tls_cert_not_after, mqtt.tls_cert_not_after]
value_name: days until the certificate expires
value_basis: from the collection's start to the soonest expiry among the server's listener certificates, negative once expired
*/
WITH names AS (
    SELECT DISTINCT unnest(expected_names || observed_names || missing_names) AS name
    FROM epochs WHERE epoch = getvariable('epoch')
),
-- Each server's newest collection within 24 hours in which VARZ answered,
-- as the Accounts page reads it.
varz AS (
    SELECT s.server_name, ee.epoch, e.started_at, ss.tls_cert_not_after, ss.cluster_tls_cert_not_after,
        ss.gateway_tls_cert_not_after, ss.leafnode_tls_cert_not_after, ss.websocket_tls_cert_not_after,
        ss.mqtt_tls_cert_not_after
    FROM epoch_endpoints ee
    JOIN epochs e ON e.epoch = ee.epoch
    JOIN servers s ON s.server_id = ee.server_id
    JOIN server_stats ss ON ss.epoch = ee.epoch AND ss.server_id = ee.server_id
    WHERE ee.endpoint = 'VARZ' AND ee.state = 'ok' AND ee.epoch <= getvariable('epoch')
      AND e.started_at >= getvariable('epoch_time') - INTERVAL 24 HOUR
      AND (s.server_name IN (SELECT name FROM names) OR ee.epoch = getvariable('epoch'))
    QUALIFY row_number() OVER (PARTITION BY s.server_name ORDER BY ee.epoch DESC) = 1
),
certs AS (
    SELECT server_name, epoch, started_at, 'client' AS listener, 'tls_cert_not_after' AS field,
        tls_cert_not_after AS not_after FROM varz
    UNION ALL SELECT server_name, epoch, started_at, 'cluster', 'cluster.tls_cert_not_after', cluster_tls_cert_not_after FROM varz
    UNION ALL SELECT server_name, epoch, started_at, 'gateway', 'gateway.tls_cert_not_after', gateway_tls_cert_not_after FROM varz
    UNION ALL SELECT server_name, epoch, started_at, 'leafnode', 'leaf.tls_cert_not_after', leafnode_tls_cert_not_after FROM varz
    UNION ALL SELECT server_name, epoch, started_at, 'websocket', 'websocket.tls_cert_not_after', websocket_tls_cert_not_after
        FROM varz
    UNION ALL SELECT server_name, epoch, started_at, 'mqtt', 'mqtt.tls_cert_not_after', mqtt_tls_cert_not_after FROM varz
),
-- Every listener certificate of the server, for the related settings.
expiries AS (
    SELECT server_name, json_group_object(field, strftime(not_after, '%Y-%m-%d %H:%M:%S UTC')) AS related_config
    FROM certs WHERE not_after IS NOT NULL
    GROUP BY server_name
),
soon AS (
    SELECT * FROM certs
    WHERE not_after IS NOT NULL AND not_after <= getvariable('epoch_time') + getvariable('warn_within')
),
per_server AS (
    SELECT server_name, any_value(epoch) AS epoch, any_value(started_at) AS started_at, min(not_after) AS soonest,
        string_agg(listener || ' certificate ' || expiry_text(not_after, getvariable('epoch_time')), '; '
            ORDER BY not_after, listener) AS listeners
    FROM soon
    GROUP BY server_name
)
SELECT
    server_name AS entity_key,
    CASE WHEN soonest <= getvariable('epoch_time') + getvariable('crit_within') THEN 'critical' ELSE 'warning' END
        AS severity,
    server_name || ': ' || listeners
        || CASE WHEN epoch < getvariable('epoch')
                THEN ' (as VARZ reported it at ' || strftime(started_at, '%Y-%m-%d %H:%M UTC') || ')' ELSE '' END
        || '.' AS detail,
    round(epoch(soonest - getvariable('epoch_time')) / 86400, 2) AS value,
    x.related_config,
    server_name AS related_source
FROM per_server JOIN expiries x USING (server_name)

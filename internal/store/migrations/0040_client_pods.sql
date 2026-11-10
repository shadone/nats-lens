-- The pods of NATS clients that the Kubernetes watcher sees when
-- kubernetes.client_pod_watch is set, one row per pod address, so that a
-- connection's client address can be matched to the pod that held it at
-- the connection's collection. Empty when the watch is off.

CREATE TABLE client_pods (
    uid           VARCHAR NOT NULL,
    ip            VARCHAR NOT NULL,
    namespace     VARCHAR NOT NULL,
    pod           VARCHAR NOT NULL,
    node          VARCHAR NOT NULL DEFAULT '',
    workload_kind VARCHAR NOT NULL DEFAULT '',
    workload      VARCHAR NOT NULL DEFAULT '',
    app_name      VARCHAR NOT NULL DEFAULT '',
    host_network  BOOLEAN NOT NULL DEFAULT false,
    first_seen    TIMESTAMP NOT NULL,
    last_seen     TIMESTAMP NOT NULL,
    ended_at      TIMESTAMP
);
CREATE UNIQUE INDEX client_pods_key ON client_pods (uid, ip);
CREATE INDEX client_pods_ip ON client_pods (ip);
COMMENT ON TABLE client_pods IS 'One row per address of a pod of a NATS client in the namespaces kubernetes.client_pod_watch lists, as the Kubernetes watcher saw it. A connection is matched to a pod when its client address (connections.ip) is the row''s ip at the connection''s epoch: first_seen <= epochs.started_at and ended_at NULL or at or after it. Addresses are reused: two rows may hold one ip at different times. A host-network pod shares its node''s address and is never matched to one pod. Swept once last_seen is before the retention window, and archived in every hour from first_seen to last_seen; a rebuild keeps the copy with the latest last_seen.';
COMMENT ON COLUMN client_pods.uid IS 'The pod''s UID; a pod recreated under the same name has a new one.';
COMMENT ON COLUMN client_pods.ip IS 'One of the pod''s addresses (status.podIPs), IPv4 or IPv6.';
COMMENT ON COLUMN client_pods.namespace IS 'The pod''s namespace.';
COMMENT ON COLUMN client_pods.pod IS 'The pod''s name.';
COMMENT ON COLUMN client_pods.node IS 'The node the pod runs on; empty before it is scheduled.';
COMMENT ON COLUMN client_pods.workload_kind IS 'Kind of the workload that owns the pod: Deployment (a ReplicaSet named after the pod-template-hash label), ReplicaSet, StatefulSet, DaemonSet, CronJob (a Job named <cronjob>-<8 or more digits>), Job, another controller''s kind, or Pod for a pod without a controller.';
COMMENT ON COLUMN client_pods.workload IS 'Name of the owning workload; the pod''s name for a bare pod.';
COMMENT ON COLUMN client_pods.app_name IS 'The pod''s app.kubernetes.io/name label; empty without one.';
COMMENT ON COLUMN client_pods.host_network IS 'True for a pod on the host network: its address is its node''s, shared with every other such pod there.';
COMMENT ON COLUMN client_pods.first_seen IS 'From when the pod held the address: its start time (status.startTime), or when the watcher first saw it with the address when that is unknown.';
COMMENT ON COLUMN client_pods.last_seen IS 'When the watcher last saw the pod live; refreshed every few minutes while it is.';
COMMENT ON COLUMN client_pods.ended_at IS 'When the pod released the address: its deletion or its end (phase Succeeded or Failed) as the watcher saw it, or last_seen when a later list no longer had the pod. NULL while the pod is live.';

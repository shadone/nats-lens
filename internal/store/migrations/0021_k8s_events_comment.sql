-- The Kubernetes watcher writes events with source k8s; the comments say so.

COMMENT ON COLUMN events.source IS 'sys: system account event; jetstream: JetStream advisory or metric; collector: written by the collector about itself (gaps, its own connections, dropped messages, Kubernetes watch gaps); finding: a finding''s transition, written with it by the checks; k8s: a Kubernetes Event, or a change of a watched NATS pod or node.';
COMMENT ON COLUMN events.kind IS 'Short normalized kind, for example client.disconnect, consumer.deleted, stream.leader_elected, server.shutdown, collector.gap, k8s.pod.killing, k8s.node.drained.';

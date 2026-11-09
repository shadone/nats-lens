package investigation

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMetricRouteImports(t *testing.T) {
	routes := map[string]string{"cpu": "cluster.cpu_percent", "memory": "cluster.mem_bytes", "connections": "cluster.connections", "messages": "cluster.in_msgs_rate", "bytes": "cluster.in_bytes_rate", "servers": "cluster.servers_up", "streams": "cluster.streams", "kv-buckets": "cluster.kv_buckets", "object-stores": "cluster.object_stores", "accounts": "cluster.accounts", "durable-consumers": "cluster.durable_consumers", "assets": "cluster.assets_replicated", "meta-changes": "cluster.meta_leader_changes", "stream-messages": "stream.messages", "stream-bytes": "stream.bytes", "stream-replica-bytes": "stream.observed_replica_bytes", "stream-ingest": "stream.msgs_rate", "consumer-pending": "consumer.num_pending", "consumer-ack-pending": "consumer.num_ack_pending", "consumer-redelivered": "consumer.num_redelivered", "consumer-delivery": "consumer.delivered_rate"}
	for route, metric := range routes {
		t.Run(route, func(t *testing.T) {
			c, err := ContextFromURL("https://lens.example.com/metrics/"+route+"?system=shop&range=1h", "https://lens.example.com", fixtureNow())
			if err != nil {
				t.Fatal(err)
			}
			if c.Metric != metric || len(c.Series) == 0 {
				t.Fatalf("missing route metric: %+v", c)
			}
			raw, _ := json.Marshal(c)
			round, err := ParseContext(strings.NewReader(string(raw)), c.Deployment)
			if err != nil || round.Metric != metric {
				t.Fatalf("context round trip: %v", err)
			}
			if !strings.Contains(c.SourcePath, "/metrics/") {
				t.Fatal(c.SourcePath)
			}
		})
	}
}
func TestMetricRouteSelectedScopeAndOptions(t *testing.T) {
	for _, tc := range []struct{ route, query, kind, key, metric string }{
		{"messages", "server=nats-east&account=APP", "ENTITY_TYPE_SERVER", "nats-east", "server.in_msgs_rate"},
		{"stream-bytes", "stream=APP%2FKV_orders&account=OTHER", "ENTITY_TYPE_STREAM", "APP/KV_orders", "stream.bytes"},
		{"consumer-pending", "consumer=APP%2Forders%2Fworker&stream=OTHER%2Fwrong&account=OTHER", "ENTITY_TYPE_CONSUMER", "APP/orders/worker", "consumer.num_pending"},
	} {
		t.Run(tc.route, func(t *testing.T) {
			c, err := ContextFromURL("https://lens.example.com/metrics/"+tc.route+"?system=shop&from=2026-10-17T13:00:00Z&to=2026-10-17T14:00:00Z&"+tc.query+"&page=2&rank=1&rankBy=maximum&rankMetric="+tc.metric+"&rankOrder=asc&rankQuery=worker", "https://lens.example.com", fixtureNow())
			if err != nil {
				t.Fatal(err)
			}
			if c.Entity == nil || c.Entity.Type != tc.kind || c.Entity.Key != tc.key || c.Metric != tc.metric {
				t.Fatalf("lost selected identity: %+v", c)
			}
			if tc.kind != "ENTITY_TYPE_SERVER" && c.Account != "APP" {
				t.Fatal(c.Account)
			}
			req, unapplied, err := Prepare("GetTimeSeriesBatch", c, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(req), tc.key) || len(unapplied) == 0 {
				t.Fatalf("missing scope or undisclosed page options: %s %v", req, unapplied)
			}
			link, err := c.Link(c.Deployment)
			if err != nil || !strings.Contains(link, "rankBy=maximum") || !strings.Contains(link, "page=2") {
				t.Fatalf("lost options: %s %v", link, err)
			}
		})
	}
	for _, path := range []string{"/metrics/not-a-metric", "/metrics/cpu/extra", "/metrics/%2e%2e", "/metrics/stream-bytes?stream=not-compound", "/metrics/consumer-pending?consumer=APP%2Forders", "/metrics/consumer-pending?stream=orders"} {
		join := "?"
		if strings.Contains(path, "?") {
			join = "&"
		}
		if _, err := ContextFromURL("https://lens.example.com"+path+join+"system=shop", "https://lens.example.com", fixtureNow()); err == nil {
			t.Fatalf("invalid metric route accepted: %s", path)
		}
	}
}

package investigation

import (
	"bytes"
	"context"
	"encoding/json"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"
	"os"
	"regexp"
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

func TestMetricRouteConsumerStreamRankingScope(t *testing.T) {
	c, err := ContextFromURL("https://lens.example.com/metrics/consumer-pending?system=shop&stream=APP%2Forders&account=OTHER&range=1h", "https://lens.example.com", fixtureNow())
	if err != nil {
		t.Fatal(err)
	}
	req, unapplied, err := Prepare("ListMetricContributors", c, nil)
	if err != nil {
		t.Fatal(err)
	}
	var r struct{ Account, Stream, Metric string }
	if err = json.Unmarshal(req, &r); err != nil {
		t.Fatal(err)
	}
	if r.Account != "APP" || r.Stream != "orders" || r.Metric != "consumer.num_pending" {
		t.Fatalf("broadened stream comparison: %s; unapplied %v", req, unapplied)
	}
}

func TestMetricRoutesMatchUIInventory(t *testing.T) {
	seen := map[string]bool{}
	pattern := regexp.MustCompile(`key:'([^']+)'[^\n]*?(?:metrics:\['|metric:')([^']+)'`)
	for _, path := range []string{"../../web/src/lib/metricNavigation.ts", "../../web/src/lib/resourceMetrics.ts", "../../web/src/lib/aggregateMetrics.ts"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range pattern.FindAllStringSubmatch(string(data), -1) {
			r, ok := metricRoutes[m[1]]
			if !ok || len(r.metrics) == 0 || r.metrics[0] != m[2] {
				t.Fatalf("UI route %s/%s is not explicitly mapped", m[1], m[2])
			}
			seen[m[1]] = true
		}
	}
	if len(seen) != len(metricRoutes) {
		t.Fatalf("UI/CLI route inventory differs: %d vs %d", len(seen), len(metricRoutes))
	}
}
func TestMetricURLAndCopiedContextThroughCommand(t *testing.T) {
	address := "https://lens.example.com/metrics/messages?system=shop&server=nats-east&range=1h&page=2&rank=1"
	copied, err := ContextFromURL(address, "https://lens.example.com", fixtureNow())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(copied)
	var first json.RawMessage
	for _, mode := range []string{"url", "copied"} {
		t.Run(mode, func(t *testing.T) {
			source := new(frozenSource)
			opener := func(context.Context, OfflineSelection) (*Session, error) { return &Session{Source: source}, nil }
			args := []string{"query", "GetTimeSeriesBatch", "-bundle", "fixture"}
			if mode == "url" {
				args = append(args, "-url", address)
			} else {
				args = append(args, "-context", "-")
			}
			var out, diagnostic bytes.Buffer
			if err := RunWithOffline(context.Background(), args, bytes.NewReader(raw), &out, &diagnostic, opener); err != nil {
				t.Fatalf("%v %s", err, out.String())
			}
			var result struct {
				Request   json.RawMessage
				Context   *Context
				Unapplied []string `json:"unappliedContextFilters"`
			}
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if len(source.requests) != 2 || !strings.Contains(string(result.Request), "server.in_msgs_rate") || !strings.Contains(string(result.Request), "server.out_msgs_rate") || !strings.Contains(string(result.Request), "nats-east") || !strings.Contains(strings.Join(result.Unapplied, ","), "page") || !strings.Contains(strings.Join(result.Unapplied, ","), "rank") {
				t.Fatalf("lost request scope/disclosure: %s", out.String())
			}
			if mode == "url" {
				first = result.Request
			} else if !equalMetricRequest(t, first, result.Request) {
				t.Fatalf("URL/copied requests differ: %s / %s", first, result.Request)
			}
			saved, _ := json.Marshal(result.Context)
			again, err := ParseContext(bytes.NewReader(saved), result.Context.Deployment)
			if err != nil {
				t.Fatal(err)
			}
			replay, _, err := Prepare("GetTimeSeriesBatch", again, nil)
			if err != nil || !equalMetricRequest(t, replay, result.Request) {
				t.Fatalf("returned machine context changed: %s %v", replay, err)
			}
		})
	}
}

func equalMetricRequest(t *testing.T, a, b []byte) bool {
	t.Helper()
	left := dynamicpb.NewMessage(Method("GetTimeSeriesBatch").Input())
	right := dynamicpb.NewMessage(Method("GetTimeSeriesBatch").Input())
	if err := protojson.Unmarshal(a, left); err != nil {
		t.Fatal(err)
	}
	if err := protojson.Unmarshal(b, right); err != nil {
		t.Fatal(err)
	}
	return proto.Equal(left, right)
}

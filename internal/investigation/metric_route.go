package investigation

import (
	"errors"
	"net/url"
	"strings"
)

// Navigation identities only. Metric meanings and calculations stay in the
// existing ListMetrics/series handlers; a route never supplies SQL or an RPC.
type metricRoute struct {
	kind    string
	metrics []string
	fleet   []string
}

var metricRoutes = map[string]metricRoute{
	"cpu":                  {"server", []string{"server.cpu_percent"}, []string{"cluster.cpu_percent"}},
	"memory":               {"server", []string{"server.mem_bytes"}, []string{"cluster.mem_bytes"}},
	"connections":          {"server", []string{"server.connections"}, []string{"cluster.connections"}},
	"messages":             {"server", []string{"server.in_msgs_rate", "server.out_msgs_rate"}, []string{"cluster.in_msgs_rate", "cluster.out_msgs_rate"}},
	"bytes":                {"server", []string{"server.in_bytes_rate", "server.out_bytes_rate"}, []string{"cluster.in_bytes_rate", "cluster.out_bytes_rate"}},
	"stream-messages":      {"stream", []string{"stream.messages"}, nil},
	"stream-bytes":         {"stream", []string{"stream.bytes"}, nil},
	"stream-replica-bytes": {"stream", []string{"stream.observed_replica_bytes"}, nil},
	"stream-ingest":        {"stream", []string{"stream.msgs_rate"}, nil},
	"consumer-pending":     {"consumer", []string{"consumer.num_pending"}, nil},
	"consumer-ack-pending": {"consumer", []string{"consumer.num_ack_pending"}, nil},
	"consumer-redelivered": {"consumer", []string{"consumer.num_redelivered"}, nil},
	"consumer-delivery":    {"consumer", []string{"consumer.delivered_rate"}, nil},
	"servers":              {"", []string{"cluster.servers_up"}, nil},
	"streams":              {"", []string{"cluster.streams"}, nil},
	"kv-buckets":           {"", []string{"cluster.kv_buckets"}, nil},
	"object-stores":        {"", []string{"cluster.object_stores"}, nil},
	"accounts":             {"", []string{"cluster.accounts"}, nil},
	"durable-consumers":    {"", []string{"cluster.durable_consumers"}, nil},
	"assets":               {"", []string{"cluster.assets_replicated", "cluster.assets_single"}, nil},
	"meta-changes":         {"", []string{"cluster.meta_leader_changes"}, nil},
}

func metricRouteAt(path string) (metricRoute, bool) {
	p := pathSegments(path)
	if len(p) != 2 || p[0] != "metrics" {
		return metricRoute{}, false
	}
	r, ok := metricRoutes[p[1]]
	return r, ok
}
func metricRouteEntity(r metricRoute, q url.Values) (*Entity, error) {
	compound := func(kind, key string, n int) (*Entity, error) {
		parts := strings.Split(key, "/")
		if len(parts) != n {
			return nil, errors.New("metric resource selector requires a typed account/stream[/consumer] identity")
		}
		for _, p := range parts {
			if p == "" {
				return nil, errors.New("empty metric resource identity")
			}
		}
		e := &Entity{Type: "ENTITY_TYPE_" + strings.ToUpper(kind), Key: key, Account: parts[0], Stream: parts[1]}
		if n == 3 {
			e.Consumer = parts[2]
		}
		return e, validateEntity(e)
	}
	if r.kind == "server" && q.Has("server") {
		e := &Entity{Type: "ENTITY_TYPE_SERVER", Key: q.Get("server"), Server: q.Get("server")}
		if e.Key == "" {
			return nil, nil
		}
		return e, validateEntity(e)
	}
	if (r.kind == "stream" || r.kind == "consumer") && q.Has(r.kind) {
		n := 2
		if r.kind == "consumer" {
			n = 3
		}
		return compound(r.kind, q.Get(r.kind), n)
	}
	// A consumer comparison can select a stream without selecting one consumer.
	if r.kind == "consumer" && q.Get("stream") != "" {
		return compound("stream", q.Get("stream"), 2)
	}
	return nil, nil
}
func (c *Context) inferMetricRoute(path string, q url.Values) error {
	r, ok := metricRouteAt(path)
	if !ok {
		return nil
	}
	e, err := metricRouteEntity(r, q)
	if err != nil {
		return err
	}
	c.Entity = e
	metrics := r.metrics
	if r.kind == "server" && e == nil {
		metrics = r.fleet
	}
	c.ChartLabel = "Metric investigation: " + pathSegments(path)[1]
	for _, metric := range metrics {
		var seriesEntity *Entity
		if e != nil && strings.EqualFold(strings.TrimPrefix(e.Type, "ENTITY_TYPE_"), r.kind) {
			seriesEntity = e
		}
		c.Series = append(c.Series, SeriesReference{Label: metric, Metric: metric, Entity: seriesEntity})
	}
	if len(metrics) > 0 {
		c.Metric = metrics[0]
	}
	return nil
}

package fake

import (
	"math"
	"strings"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// example-staging is a three-server cluster that nats-lens began watching
// nine hours ago, and whose collector stopped collecting three hours ago:
// NATS closed its connection after repeated authorization errors when the
// credentials were rotated. Its history is younger than the default range,
// and its data ends at the close; nothing newer is stored.

// stagingStale is how long before now the data ends; stagingHistory how
// long before now the history begins.
const (
	stagingStale   = 3 * time.Hour
	stagingHistory = 9 * time.Hour
)

// stagingStop is when the collector's connection closed, and stagingStart
// when its first epoch was stored, in the fixed frame.
var (
	stagingStop  = FixedNow.Add(-stagingStale)
	stagingStart = FixedNow.Add(-stagingHistory)
)

// stagingScenario is example-staging.
func stagingScenario() *scenario {
	server := func(name, id string, conns int64) serverSpec {
		return serverSpec{name: name, id: id, oldID: id + "OLD", day: 9, h: 7, m: 30, conns: conns, cluster: StagingSystem,
			version: Version, store: 2 * gib, maxStore: 20 * gib, mem: 96 * mib, cpu: 1.5}
	}
	return &scenario{
		cluster: StagingSystem,
		servers: []serverSpec{
			server("nats-0", "NSTAGEFAKE0SERVER", 6),
			server("nats-1", "NSTAGEFAKE1SERVER", 5),
			server("nats-2", "NSTAGEFAKE2SERVER", 5),
		},
		streams: []streamSpec{
			{name: "order-jobs", msgs: 1204, bytes: 1.6 * mib, perDay: 120 * kib, consumers: []string{"resize-worker", "index-worker"}},
			{name: "catalog-changes", msgs: 8840, bytes: 12 * mib, perDay: 400 * kib, consumers: []string{"catalog-applier"}},
			{name: "KV_customer-config", msgs: 31, bytes: 14 * kib, perDay: 1 * kib, ephemeral: 3},
		},
		clients:         []string{"order-service", "catalog-api", "resize-worker"},
		metaLeader:      "nats-1",
		epoch:           911,
		stale:           stagingStale,
		history:         stagingHistory,
		collection:      v1.CollectionState_COLLECTION_STATE_CONNECTION_CLOSED,
		collectionError: "nats: authorization violation",
		errorSince:      stagingStop,
		routineClient:   "catalog-api",
		findings:        stagingFindings,
		events:          stagingEvents,
		rows:            stagingRows,
		sample:          stagingSample,
	}
}

func stagingFindings(s scen) []*v1.Finding {
	// The stream had no limit before nats-lens watched it: first seen at
	// the start of the history.
	at := stagingStart.Add(s.shift)
	f := finding("stream-005", "Stream without any limit", info, firing, streamRef("catalog-changes"), at, at,
		"max_msgs -1, max_bytes -1, max_age 0: nothing bounds the stream.")
	f.RelatedConfig = cfg([][2]string{{"max_msgs", "-1"}, {"max_bytes", "-1"}, {"max_age", "0"},
		{"max_msgs_per_subject", "-1"}, {"allow_msg_ttl", "false"}}, "max_bytes")
	f.RelatedConfigSource = "nats-1"
	return []*v1.Finding{f}
}

// stagingEvents are the connection's close and routine reconnects before.
func stagingEvents(s scen, _ []*v1.Finding) []*v1.Event {
	stop := stagingStop.Add(s.shift)
	collector := func(at time.Time, kind, reason, summary string) *v1.Event {
		return &v1.Event{Time: ts(at), Source: v1.EventSource_EVENT_SOURCE_COLLECTOR, Kind: kind, Type: "", Server: "nats-1",
			Summary: summary + reason}
	}
	out := []*v1.Event{
		collector(stop, "collector.connection_closed", "nats: authorization violation", "collector connection collector closed: "),
		collector(stop.Add(-2*time.Second), "collector.disconnected", "nats: authorization violation", "collector connection collector lost server nats-1: "),
	}
	// Routine reconnects every 17 minutes, from the start of the history.
	for k := range int((stagingStop.Sub(stagingStart) - time.Minute) / (17 * time.Minute)) {
		kind, typ := "client.connect", "CLIENT.CONNECT"
		if k%2 == 0 {
			kind, typ = "client.disconnect", "CLIENT.DISCONNECT"
		}
		out = append(out, &v1.Event{
			Time: ts(stop.Add(-time.Duration(k+1) * 17 * time.Minute)), Source: srcNATS, Kind: kind, Type: typ,
			Server: s.sc.servers[k%3].name, Account: Account, ClientName: "catalog-api", ClientHost: "10.40.1.12",
			Summary: "catalog-api " + strings.TrimPrefix(kind, "client.") + " from 10.40.1.12",
		})
	}
	return out
}

func stagingRows(*scenario) []rowSpec {
	return []rowSpec{
		{"stream-005", "Stream without any limit", info, []*v1.EntityRef{streamRef("catalog-changes")},
			[]span{{stagingStart, stagingStop}}},
	}
}

// stagingSample draws quiet traffic; the series end where the data ends.
func stagingSample(sc *scenario, metric string, e *v1.EntityRef, c time.Time) (float64, bool) {
	days := FixedNow.Sub(c).Hours() / 24
	switch metric {
	case "cluster.servers_up":
		return 3, true
	case "cluster.connections":
		return math.Round(16 + noise(metric, c)*4), true
	case "cluster.in_msgs_rate":
		return math.Round(12 + noise(metric, c)*6), true
	case "cluster.out_msgs_rate":
		return math.Round(30 + noise(metric, c)*10), true
	case "cluster.in_bytes_rate", "cluster.out_bytes_rate", "server.in_bytes_rate", "server.out_bytes_rate":
		v, ok := stagingSample(sc, strings.Replace(metric, "_bytes_", "_msgs_", 1), e, c)
		return math.Round(v * 900), ok
	case "cluster.js_storage_bytes":
		return 6*gib - days*30*mib, true
	case "cluster.durable_consumers":
		return 3, true
	case "server.connections", "server.in_msgs_rate", "server.out_msgs_rate":
		v, _ := stagingSample(sc, "cluster."+strings.TrimPrefix(metric, "server."), e, c)
		return math.Round(v / 3), true
	case "server.mem_bytes":
		return 96*mib + noise(metric+e.GetKey(), c)*8*mib, true
	case "server.cpu_percent":
		return 1.5 + noise(metric+e.GetKey(), c), true
	case "server.js_storage_bytes":
		return 2*gib - days*10*mib, true
	case "stream.bytes", "stream.messages":
		st, _ := sc.findStream(e.GetAccount(), e.GetStream())
		b := math.Max(st.bytes-days*st.perDay, st.bytes*0.05)
		if metric == "stream.bytes" {
			return b, true
		}
		return math.Round(float64(st.msgs) * b / st.bytes), true
	case "consumer.num_pending", "consumer.num_ack_pending":
		return math.Round(3 + noise(metric+e.GetKey(), c)*4), true
	case "consumer.num_redelivered", "cluster.meta_leader_changes", "server.slow_consumers":
		return 0, true
	case "consumer.delivered_rate":
		return math.Round((0.4+noise(metric+e.GetKey(), c)*0.3)*10) / 10, true
	}
	return 0, false
}

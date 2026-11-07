package storeprov_test

import (
	"strconv"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// With two servers claiming the lead, the stream and consumer details list
// each server once, with the lag and state that nats-0, the leader chosen
// by name, reports, whichever claimant's list is read first.
func TestDetailsWithTwoClaimants(t *testing.T) {
	for _, loserFirst := range []bool{true, false} {
		st := emptyStore(t)
		r := rows{t, st}
		ids := []string{"S0", "S1", "S2"}
		for i, id := range ids {
			r.server(id, "nats-"+strconv.Itoa(i))
		}
		r.epoch(1, t0, ids...)
		r.add("streams", map[string]any{"account": "APP", "stream": "orders", "first_epoch": 1, "last_epoch": 1})
		r.add("consumers", map[string]any{"account": "APP", "stream": "orders", "consumer": "ship", "first_epoch": 1, "last_epoch": 1})
		for _, id := range ids {
			r.add("stream_stats", map[string]any{"epoch": 1, "server_id": id, "account": "APP", "stream": "orders", "is_leader": id != "S1", "messages": 10})
			r.add("consumer_stats", map[string]any{"epoch": 1, "server_id": id, "account": "APP", "stream": "orders", "consumer": "ship",
				"is_leader": id != "S1", "delivered_stream_seq": 5})
		}
		leads := []struct {
			id      string
			current bool
			lag     int64
		}{{"S0", false, 9}, {"S2", true, 0}}
		if loserFirst {
			leads[0], leads[1] = leads[1], leads[0]
		}
		for _, l := range leads {
			r.add("stream_replicas", map[string]any{"epoch": 1, "server_id": l.id, "account": "APP", "stream": "orders", "peer_name": "nats-1",
				"current": l.current, "offline": false, "lag": l.lag})
			r.add("consumer_replicas", map[string]any{"epoch": 1, "server_id": l.id, "account": "APP", "stream": "orders", "consumer": "ship",
				"peer_name": "nats-1", "current": l.current, "offline": false, "lag": l.lag})
		}
		c := serveSeries(t, st, t0.Add(30*time.Second))
		rg := &v1.TimeRange{Start: timestamppb.New(t0.Add(-time.Hour)), End: timestamppb.New(t0.Add(30 * time.Second))}
		sv := must(c.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{Account: "APP", Stream: "orders", Range: rg}))).GetServers()
		cv := must(c.GetConsumer(ctx, connect.NewRequest(&v1.GetConsumerRequest{Account: "APP", Stream: "orders", Consumer: "ship", Range: rg}))).GetServers()
		type view interface {
			GetServer() string
			GetLag() int64
			GetCurrent() bool
		}
		check := func(what string, vs []view) {
			seen := map[string]int{}
			for _, v := range vs {
				seen[v.GetServer()]++
				if v.GetServer() == "nats-1" && (v.GetLag() != 9 || v.GetCurrent()) {
					t.Errorf("loser first %v: %s nats-1 lag %d current %v, want nats-0's 9, not current", loserFirst, what, v.GetLag(), v.GetCurrent())
				}
			}
			if len(vs) != 3 || seen["nats-1"] != 1 {
				t.Errorf("loser first %v: %s lists %v", loserFirst, what, seen)
			}
		}
		var s, k []view
		for _, v := range sv {
			s = append(s, v)
		}
		for _, v := range cv {
			k = append(k, v)
		}
		check("stream", s)
		check("consumer", k)
	}
}

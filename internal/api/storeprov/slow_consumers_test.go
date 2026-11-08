package storeprov_test

import (
	"connectrpc.com/connect"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

func TestSlowConsumerInvestigationIntervalsAndIndependentEvidence(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	r.server("S1", "nats-0")
	for i, v := range []int{0, 5, 8, 1, 40, 42} {
		id := "S0"
		if i >= 4 {
			id = "S1"
		}
		r.epoch(int64(i+1), t0.Add(time.Duration(i)*30*time.Second), id)
		r.add("server_stats", map[string]any{"epoch": i + 1, "server_id": id, "slow_consumers": v})
	}
	for i := 0; i < 2; i++ {
		at := t0.Add(time.Duration(40+i) * time.Second)
		r.add("events", map[string]any{"id": i + 1, "time": at, "received_at": at, "source": "sys", "kind": "client.disconnect", "server_name": "nats-0", "account": "SHOP", "client_name": "checkout@one", "reason": "Slow Consumer (Write Deadline)", "summary": "checkout disconnected", "raw": `{"server":{"id":"S0"},"client":{"id":12}}`})
	}
	r.add("finding_transitions", map[string]any{"finding_id": "slow-server", "check_code": "server-009", "entity_type": "server", "entity_key": "nats-0", "epoch": 2, "time": t0.Add(30 * time.Second), "kind": "firing", "severity": "warning", "detail": "5 detections"})
	r.add("finding_transitions", map[string]any{"finding_id": "slow-client", "check_code": "client-003", "entity_type": "client", "entity_key": "checkout@one", "epoch": 3, "time": t0.Add(60 * time.Second), "kind": "resolved", "severity": "warning", "detail": "client evidence"})
	c := serveSeries(t, st, t0.Add(3*time.Minute))
	req := &v1.GetSlowConsumerInvestigationRequest{Range: &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(3 * time.Minute))}}
	res := must(c.GetSlowConsumerInvestigation(ctx, connect.NewRequest(req)))
	if res.Detections != 10 || res.RecordedDisconnects != 2 || len(res.Events) != 2 || len(res.Findings) != 2 {
		t.Fatalf("totals %v", res)
	}
	if len(res.Observations) != 6 {
		t.Fatalf("observations %v", res.Observations)
	}
	// Newest first. A new process's initial 40 and a reset from 8 to 1 count nothing.
	if res.Observations[0].GetIncrease() != 2 || res.Observations[1].Increase != nil || res.Observations[2].Increase != nil {
		t.Fatalf("restart/reset %v", res.Observations)
	}
	o := res.Observations[4]
	if o.ServerId != "S0" || o.Epoch != 2 || !o.IntervalStart.AsTime().Equal(t0) || !o.IntervalEnd.AsTime().Equal(t0.Add(30*time.Second)) {
		t.Fatalf("interval %v", o)
	}
	if res.Events[0].ConnectionId != "12" || res.Events[0].ServerId != "S0" {
		t.Fatalf("identity %v", res.Events)
	}
	if !res.EventsCaptured || res.EventsStart == nil || res.AttributionLimit == "" || res.Provenance == "" {
		t.Fatalf("coverage %v", res)
	}
	req.Account = "OTHER"
	req.Limit = 1
	res = must(c.GetSlowConsumerInvestigation(ctx, connect.NewRequest(req)))
	if res.Detections != 10 || res.RecordedDisconnects != 0 || len(res.Events) != 0 || len(res.Observations) != 1 || !res.ObservationsTruncated {
		t.Fatalf("bounded account evidence %v", res)
	}
}

func TestSlowConsumerInvestigationGapsCounterOnlyAndBounds(t *testing.T) {
	for _, zone := range []string{"UTC", "America/New_York", "Asia/Tokyo"} {
		t.Run(zone, func(t *testing.T) {
			t.Setenv("TZ", zone)
			st := emptyStore(t)
			r := rows{t, st}
			r.server("S0", "nats-0")
			for i := 0; i < 5; i++ {
				r.epoch(int64(i+1), t0.Add(time.Duration(i)*30*time.Second), "S0")
				if i != 2 {
					r.add("server_stats", map[string]any{"epoch": i + 1, "server_id": "S0", "slow_consumers": i * 3})
				}
			}
			if _, err := st.DB().Exec("UPDATE epochs SET partial=true, missing_names=['nats-0'] WHERE epoch=3"); err != nil {
				t.Fatal(err)
			}
			c := serveSeries(t, st, t0.Add(3*time.Minute))
			req := &v1.GetSlowConsumerInvestigationRequest{Range: &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(3 * time.Minute))}, Server: "nats-0"}
			res := must(c.GetSlowConsumerInvestigation(ctx, connect.NewRequest(req)))
			if res.IncompleteCollections != 1 || res.Detections != 12 || res.EventsCaptured || !res.CaptureGaps || len(res.Events) != 0 {
				t.Fatalf("counter-only with report gap %v", res)
			}
			found := false
			for _, o := range res.Observations {
				if o.Epoch == 4 {
					found = o.Increase != nil && o.GetIncrease() == 6 && o.IntervalEnd.AsTime().Sub(o.IntervalStart.AsTime()) == time.Minute
				}
			}
			if !found {
				t.Fatalf("missing report interval: %v", res)
			}
			req.Range.End = timestamppb.New(t0.Add(31 * 24 * time.Hour))
			if _, err := c.GetSlowConsumerInvestigation(ctx, connect.NewRequest(req)); codeOf(err) != connect.CodeInvalidArgument {
				t.Fatalf("range bound: %v", err)
			}
		})
	}
}

func TestSlowConsumerInvestigationIncompleteCaptureAndEventLimit(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	r.epoch(1, t0, "S0")
	r.epoch(2, t0.Add(30*time.Second), "S0")
	r.add("server_stats", map[string]any{"epoch": 1, "server_id": "S0", "slow_consumers": 0})
	r.add("server_stats", map[string]any{"epoch": 2, "server_id": "S0", "slow_consumers": 7})
	r.add("events", map[string]any{"id": 1, "time": t0.Add(-time.Hour), "received_at": t0.Add(10 * time.Second), "source": "collector", "kind": "collector.gap", "summary": "capture stopped"})
	for i := 2; i < 5; i++ {
		at := t0.Add(time.Duration(i) * time.Second)
		r.add("events", map[string]any{"id": i, "time": at, "received_at": at, "source": "sys", "kind": "client.disconnect", "server_name": "nats-0", "reason": "Slow Consumer (Pending Bytes)", "summary": "disconnect"})
	}
	c := serveSeries(t, st, t0.Add(time.Minute))
	req := &v1.GetSlowConsumerInvestigationRequest{Range: &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(time.Minute))}, Limit: 1}
	res := must(c.GetSlowConsumerInvestigation(ctx, connect.NewRequest(req)))
	if !res.CaptureGaps || !res.EventsCaptured || res.RecordedDisconnects != 3 || !res.EventsTruncated || len(res.Events) != 1 || res.Events[0].Event.Id != "4" || res.Events[0].ConnectionId != "" || res.Events[0].ServerId != "" {
		t.Fatalf("capture/identity/limit %v", res)
	}
	req.Limit = -1
	if _, err := c.GetSlowConsumerInvestigation(ctx, connect.NewRequest(req)); codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("negative limit %v", err)
	}
}

func TestSlowConsumerInvestigationLongGapMatchesChart(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	for i, seconds := range []int{0, 30, 300, 330} {
		r.epoch(int64(i+1), t0.Add(time.Duration(seconds)*time.Second), "S0")
		r.add("server_stats", map[string]any{"epoch": i + 1, "server_id": "S0", "slow_consumers": i * 4})
	}
	c := serveSeries(t, st, t0.Add(6*time.Minute))
	rng := &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(6 * time.Minute))}
	res := must(c.GetSlowConsumerInvestigation(ctx, connect.NewRequest(&v1.GetSlowConsumerInvestigationRequest{Range: rng})))
	series := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Range: rng, Metric: "cluster.slow_consumers"})))
	var total int64
	for _, p := range series.GetSeries().GetPoints() {
		total += int64(p.Value)
	}
	if res.Detections != 8 || res.Detections != total {
		t.Fatalf("investigation %d, chart %d", res.Detections, total)
	}
	for _, o := range res.Observations {
		if o.Epoch == 3 && (o.Increase != nil || o.UnavailableReason == "") {
			t.Fatalf("long gap %v", o)
		}
	}
}

// All entry surfaces must select report endpoints in [start,end), while a
// bounded pre-range report may establish the first endpoint's increase.
func TestSlowConsumerInvestigationAgreement(t *testing.T) {
	for _, cadence := range []time.Duration{30 * time.Second, time.Minute, 5 * time.Minute} {
		t.Run(cadence.String(), func(t *testing.T) {
			st := emptyStore(t)
			r := rows{t, st}
			r.server("S0", "nats-0")
			r.server("S1", "nats-0")
			values := []any{0, 5, 8, nil, 12, 1, 4, 20, 22}
			offsets := []int{0, 1, 2, 3, 4, 5, 9, 10, 11}
			for i, v := range values {
				id := "S0"
				if i >= 7 {
					id = "S1"
				}
				r.epoch(int64(i+1), t0.Add(time.Duration(offsets[i])*cadence), id)
				r.add("server_stats", map[string]any{"epoch": i + 1, "server_id": id, "slow_consumers": v})
			}
			if _, err := st.DB().Exec(`UPDATE servers SET last_epoch=9,last_seen_at=?`, t0.Add(11*cadence)); err != nil {
				t.Fatal(err)
			}
			_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: cadence, Live: true, Clock: func() time.Time { return t0.Add(12 * cadence) }})
			for _, window := range []struct {
				name       string
				start, end time.Duration
				want       int64
			}{
				{"custom interior", cadence / 2, 3 * cadence, 8},
				{"fixed narrow endpoint", 2 * cadence, 2*cadence + time.Second, 3},
				{"exclusive endpoint", cadence, 2 * cadence, 5},
				{"missing counter reset gap restart", 0, 12 * cadence, 10},
				{"long gap", 9 * cadence, 10 * cadence, 0},
			} {
				t.Run(window.name, func(t *testing.T) {
					rng := &v1.TimeRange{Start: timestamppb.New(t0.Add(window.start)), End: timestamppb.New(t0.Add(window.end))}
					inv := must(c.GetSlowConsumerInvestigation(ctx, connect.NewRequest(&v1.GetSlowConsumerInvestigationRequest{Range: rng})))
					list := must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{Range: rng})))
					var headline int64
					for _, s := range list.Servers {
						headline += s.GetSlowConsumersInRange()
					}
					if inv.Detections != window.want || headline != window.want {
						t.Fatalf("want %d investigation=%d headline=%d", window.want, inv.Detections, headline)
					}
					for _, metric := range []string{"cluster.slow_consumers", "server.slow_consumers"} {
						req := &v1.GetTimeSeriesRequest{Range: rng, Metric: metric}
						if metric == "server.slow_consumers" {
							req.Entity = serverRef("nats-0")
						}
						chart := must(c.GetTimeSeries(ctx, connect.NewRequest(req)))
						var sum int64
						for _, p := range chart.Series.Points {
							sum += int64(p.Value)
						}
						if sum != window.want {
							t.Fatalf("%s=%d want %d", metric, sum, window.want)
						}
					}
				})
			}
		})
	}
}

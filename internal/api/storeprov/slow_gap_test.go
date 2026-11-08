package storeprov_test

import (
	"database/sql"
	"github.com/nats-io/nats-server/v2/server"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/collect"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// A partial report with no counter breaks the baseline, even when other
// endpoints answered. It is different from a wholly absent server report.
func TestSlowConsumersAcrossAMissingCount(t *testing.T) {
	st := detailStoreWith(t, false, detailOpts{epochs: 3, skip: skipIn(1, "nats-0", collect.Varz)})
	var counter sql.NullInt64
	if err := st.DB().QueryRow(`SELECT slow_consumers FROM server_stats JOIN servers USING(server_id) WHERE epoch=2 AND server_name='nats-0'`).Scan(&counter); err != nil {
		t.Fatal(err)
	}
	if counter.Valid {
		t.Fatalf("missing VARZ counter = %v, want stored NULL", counter)
	}
	t.Log("missing VARZ stores a server_stats row with NULL slow_consumers")
	_, c := serve(t, st)
	sv := must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{Range: hour()})))
	if n0 := sv.GetServers()[0]; n0.SlowConsumersInRange == nil || n0.GetSlowConsumersInRange() != 0 {
		t.Fatalf("nats-0 slow consumers in range = %d (set %v), want measured total 0 (0, NULL, 6; no comparable pair)", n0.GetSlowConsumersInRange(), n0.SlowConsumersInRange != nil)
	}
}

// Exercise the actual snapshot writer and HTTP API together: synthetic SQL rows
// alone cannot establish whether a missed endpoint means NULL or no row.
func TestSlowConsumerSnapshotGapContracts(t *testing.T) {
	for _, tc := range []struct {
		name                string
		nullReport, restart bool
		gap                 time.Duration
		wantFirst           int64
		reason              string
	}{
		{name: "missing VARZ", nullReport: true, gap: time.Minute, reason: "Counter not reported"},
		{name: "absent report", gap: time.Minute, wantFirst: 6},
		{name: "absent report at maximum gap", gap: 90 * time.Second, wantFirst: 6},
		{name: "absent report beyond maximum gap", gap: 90*time.Second + time.Millisecond, reason: "Server report or collection gap exceeds three intervals"},
		{name: "new process after absent report", restart: true, gap: time.Minute, reason: "Initial process report; no baseline"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := emptyStore(t)
			for i, offset := range []time.Duration{0, 30 * time.Second, tc.gap, tc.gap + 30*time.Second, tc.gap + 60*time.Second} {
				snap := collect.NewSnapshot(t0.Add(offset))
				snap.Expected = []string{"nats-0"}
				snap.FinishedAt = snap.StartedAt.Add(100 * time.Millisecond)
				id := "GAP0"
				if tc.restart && i >= 2 {
					id = "GAP1"
				}
				if i != 1 || tc.nullReport {
					// JSZ alone creates a report but cannot supply the process counter.
					if err := snap.Apply(collect.Jsz, envelope(t, "nats-0", id, server.JSInfo{ID: id})); err != nil {
						t.Fatal(err)
					}
					if i != 1 {
						counter := int64(0)
						if i == 2 {
							counter = 6
						}
						if i >= 3 {
							counter = 9
						}
						if err := snap.Apply(collect.Varz, envelope(t, "nats-0", id, server.Varz{ID: id, Name: "nats-0", Start: t0.Add(-time.Hour), SlowConsumers: counter})); err != nil {
							t.Fatal(err)
						}
					}
				}
				if _, err := st.WriteSnapshot(ctx, snap); err != nil {
					t.Fatal(err)
				}
			}
			var reports, nulls int
			if err := st.DB().QueryRow(`SELECT count(*), count(*) FILTER (WHERE slow_consumers IS NULL) FROM server_stats WHERE epoch=2`).Scan(&reports, &nulls); err != nil {
				t.Fatal(err)
			}
			wantRows := 0
			if tc.nullReport {
				wantRows = 1
			}
			if reports != wantRows || nulls != wantRows {
				t.Fatalf("middle collection: reports=%d NULL counters=%d want %d each", reports, nulls, wantRows)
			}
			t.Logf("middle collection: reports=%d NULL counters=%d", reports, nulls)
			c := serveSeries(t, st, t0.Add(tc.gap+90*time.Second))
			fullRange := &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(tc.gap + 90*time.Second))}
			full := must(c.GetSlowConsumerInvestigation(ctx, connect.NewRequest(&v1.GetSlowConsumerInvestigationRequest{Range: fullRange, Server: "nats-0"})))
			if full.IncompleteCollections != 1 {
				t.Fatalf("incomplete collections=%d want 1", full.IncompleteCollections)
			}
			var returned, missingCounter bool
			for _, o := range full.Observations {
				if o.Epoch == 2 && o.Increase == nil && o.UnavailableReason == "Counter not reported" {
					missingCounter = true
				}
				if o.Epoch == 3 {
					returned = true
					if o.UnavailableReason != tc.reason || (o.Increase == nil) != (tc.reason != "") || o.GetIncrease() != tc.wantFirst {
						t.Fatalf("full-range return: %v", o)
					}
				}
			}
			if !returned || missingCounter != tc.nullReport {
				t.Fatalf("full-range evidence: %v", full.Observations)
			}
			for _, window := range []struct {
				name   string
				offset time.Duration
				want   int64
				reason string
				epoch  int64
			}{
				{"first return", tc.gap, tc.wantFirst, tc.reason, 3},
				{"recovery", tc.gap + 30*time.Second, 3, "", 4},
				{"measured zero", tc.gap + 60*time.Second, 0, "", 5},
			} {
				t.Run(window.name, func(t *testing.T) {
					rng := &v1.TimeRange{Start: timestamppb.New(t0.Add(window.offset)), End: timestamppb.New(t0.Add(window.offset + time.Second))}
					list := must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{Range: rng})))
					if len(list.Servers) != 1 || list.Servers[0].SlowConsumersInRange == nil || list.Servers[0].GetSlowConsumersInRange() != window.want {
						t.Fatalf("summary want %d: %v", window.want, list.Servers)
					}
					detail := must(c.GetServer(ctx, connect.NewRequest(&v1.GetServerRequest{Name: "nats-0", Range: rng})))
					if detail.Server.SlowConsumersInRange == nil || detail.Server.GetSlowConsumersInRange() != window.want {
						t.Fatalf("detail want %d: %v", window.want, detail.Server)
					}
					for _, metric := range []string{"cluster.slow_consumers", "server.slow_consumers"} {
						var entity *v1.EntityRef
						if metric == "server.slow_consumers" {
							entity = serverRef("nats-0")
						}
						chart := seriesOf(t, c, metric, entity, rng)
						var total int64
						for _, p := range chart.Points {
							total += int64(p.Value)
						}
						if total != window.want {
							t.Fatalf("%s=%d want %d", metric, total, window.want)
						}
					}
					inv := must(c.GetSlowConsumerInvestigation(ctx, connect.NewRequest(&v1.GetSlowConsumerInvestigationRequest{Range: rng, Server: "nats-0"})))
					if inv.Detections != window.want {
						t.Fatalf("investigation=%d want %d", inv.Detections, window.want)
					}
					if window.reason != "" {
						// A narrow range's bounded lookback excludes an older baseline.
						if tc.gap > 90*time.Second {
							window.reason = "Initial process report; no baseline"
						}
						if len(inv.Observations) != 1 || inv.Observations[0].Increase != nil || inv.Observations[0].UnavailableReason != window.reason {
							t.Fatalf("unknown interval must retain reason %q: %v", window.reason, inv.Observations)
						}
					} else if window.want == 0 {
						// A comparable unchanged counter has no unavailable observation.
						if len(inv.Observations) != 0 {
							t.Fatalf("measured zero has unknown evidence: %v", inv.Observations)
						}
					} else {
						if len(inv.Observations) != 1 {
							t.Fatalf("positive interval: %v", inv.Observations)
						}
						o := inv.Observations[0]
						wantStart := t0
						if window.epoch == 4 {
							wantStart = t0.Add(tc.gap)
						}
						if o.Epoch != window.epoch || o.Increase == nil || o.GetIncrease() != window.want || o.UnavailableReason != "" || !o.IntervalStart.AsTime().Equal(wantStart) || !o.IntervalEnd.AsTime().Equal(t0.Add(window.offset)) {
							t.Fatalf("observed interval: %v", o)
						}
					}
				})
			}
		})
	}
}

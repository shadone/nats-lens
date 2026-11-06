package storeprov_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestServerHistoryMissingReports(t *testing.T) {
	for _, zone := range []string{"UTC", "Europe/Oslo"} {
		t.Run(zone, func(t *testing.T) {
			st := emptyStore(t)
			r := rows{t, st}
			if _, err := st.DB().Exec("SET TimeZone = '" + zone + "'"); err != nil {
				t.Fatal(err)
			}
			r.server("old", "nats-0")
			r.server("new", "nats-0")
			r.server("other", "nats-1")
			for i := int64(0); i < 6; i++ {
				r.epoch(i+1, t0.Add(time.Duration(i)*30*time.Second), "other")
			}
			// Explicit missing name, then both metric sources fail, then a restarted
			// process answers VARZ despite STATSZ missing. Unrelated JSZ is irrelevant.
			for _, n := range []int{2, 5} {
				if _, err := st.DB().Exec("UPDATE epochs SET missing_names=['nats-0'] WHERE epoch=?", n); err != nil {
					t.Fatal(err)
				}
			}
			for _, ep := range []string{"STATSZ", "VARZ"} {
				r.add("epoch_endpoints", map[string]any{"epoch": 3, "server_id": "old", "endpoint": ep, "state": "error"})
			}
			r.add("epoch_endpoints", map[string]any{"epoch": 4, "server_id": "new", "endpoint": "VARZ", "state": "ok"})
			r.add("epoch_endpoints", map[string]any{"epoch": 4, "server_id": "new", "endpoint": "STATSZ", "state": "missing"})
			p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Clock: func() time.Time { return t0.Add(3 * time.Minute) }}
			req := &v1.GetDataHistoryRequest{Range: &v1.TimeRange{Start: timestamppb.New(t0.Add(45 * time.Second)), End: timestamppb.New(t0.Add(135 * time.Second))}}
			// Unknown fields are deliberately discarded so this regression runs against
			// the pre-feature wire schema and fails on behavior, rather than compilation.
			if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(`{"server":"nats-0"}`), req); err != nil {
				t.Fatal(err)
			}
			req.Range = &v1.TimeRange{Start: timestamppb.New(t0.Add(45 * time.Second)), End: timestamppb.New(t0.Add(135 * time.Second))}
			f, err := p.DataHistory(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := protojson.Marshal(api.FinishDataHistory(f))
			if err != nil {
				t.Fatal(err)
			}
			var out struct {
				ServerGaps []struct{ Start, End, Reason string }
			}
			if err = json.Unmarshal(raw, &out); err != nil {
				t.Fatal(err)
			}
			if len(out.ServerGaps) != 3 {
				t.Fatalf("server gaps = %s; want three bounded missing-report intervals", raw)
			}
			want := [][2]time.Time{{t0.Add(45 * time.Second), t0.Add(time.Minute)}, {t0.Add(time.Minute), t0.Add(90 * time.Second)}, {t0.Add(120 * time.Second), t0.Add(135 * time.Second)}}
			for i, g := range out.ServerGaps {
				if g.Start != want[i][0].Format(time.RFC3339) || g.End != want[i][1].Format(time.RFC3339) || g.Reason == "" {
					t.Errorf("gap %d = %+v, want %v", i, g, want[i])
				}
			}
			if len(api.FinishDataHistory(f).GetGaps()) != 0 {
				t.Fatal("server absence became global outage")
			}
		})
	}
}

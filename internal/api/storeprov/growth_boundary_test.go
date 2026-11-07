package storeprov_test

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Enough real observations must survive reduction; an entity first seen
// inside the window must not borrow another entity's older observations.
func TestGrowthSixHourBoundary(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	first := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	r.add("servers", map[string]any{"server_id": "S0", "server_name": "nats-0", "jetstream": true, "first_epoch": 1, "last_epoch": 721, "first_seen_at": first, "last_seen_at": first.Add(6 * time.Hour)})
	queries := []string{
		`INSERT INTO epochs SELECT i+1, make_timestamp(?::BIGINT) + to_seconds(i * 30), make_timestamp(?::BIGINT) + to_seconds(i * 30 + 1), 0, 1, 1, ['nats-0'], [], [], 0, false, [], NULL FROM range(721) t(i)`,
		`INSERT INTO server_stats (epoch, server_id, js_enabled, js_storage) SELECT i+1, 'S0', true, 100000+i*30 FROM range(721) t(i)`,
		`INSERT INTO stream_stats (epoch, server_id, account, stream, is_leader, bytes, messages) SELECT i+1, 'S0', 'APP', 'stream-' || j, true, 100000+i*30, 1000+i*30 FROM range(721) t(i), range(64) s(j)`,
		`INSERT INTO stream_stats (epoch, server_id, account, stream, is_leader, bytes, messages) SELECT i+1, 'S0', 'APP', 'young', true, 100000+i*30, 1000+i*30 FROM range(721) t(i) WHERE i >= 2`,
	}
	for i, q := range queries {
		var args []any
		if i == 0 {
			args = []any{first.UnixMicro(), first.UnixMicro()}
		}
		if _, err := st.DB().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second}
	end := first.Add(6*time.Hour + 15*time.Second)
	for _, d := range []time.Duration{6 * time.Hour, 7 * time.Hour, 24 * time.Hour, time.Hour, 6*time.Hour - time.Nanosecond} {
		t.Run(fmt.Sprint(d), func(t *testing.T) {
			rg := &v1.TimeRange{Start: timestamppb.New(end.Add(-d)), End: timestamppb.New(end)}
			facts, err := p.Capacity(ctx, &v1.GetCapacityRequest{Range: rg})
			if err != nil {
				t.Fatal(err)
			}
			want := d >= 6*time.Hour
			if got := facts.Servers[0].PerDay; (got != nil) != want || got != nil && math.Abs(*got-86400) > 1 {
				t.Errorf("server growth=%v, want available %v", got, want)
			}
			for _, s := range facts.Streams {
				expected := want && s.Stream != "young"
				if (s.BytesPerDay != nil) != expected || (s.MsgsPerDay != nil) != expected {
					t.Errorf("%s bytes=%v msgs=%v, want available %v", s.Stream, s.BytesPerDay, s.MsgsPerDay, expected)
				}
			}
			if want && !facts.GrowthFrom.Equal(first) {
				t.Errorf("actual growth start %v, want %v", facts.GrowthFrom, first)
			}
			list, err := p.ListStreams(ctx, &v1.ListStreamsRequest{Range: rg})
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range list.Streams {
				expected := want && s.Entity.Stream != "young"
				if (s.BytesPerDay != nil) != expected {
					t.Errorf("Streams %s availability=%v, want %v", s.Entity.Stream, s.BytesPerDay, expected)
				}
			}
		})
	} // A prior observation plus small endpoint jitter must still permit a
	// six-hour view of a mature collector. One nominal interval is not enough.
	if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (0,?,?,0,1,1,['nats-0'],[],[],0,false,[],NULL)`, first.Add(-30*time.Second), first.Add(-29*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`INSERT INTO server_stats (epoch,server_id,js_enabled,js_storage) VALUES (0,'S0',true,99970)`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`UPDATE epochs SET started_at=started_at+INTERVAL 1 SECOND WHERE epoch=1`); err != nil {
		t.Fatal(err)
	}

	rg := &v1.TimeRange{Start: timestamppb.New(first.Add(15 * time.Second)), End: timestamppb.New(end)}
	facts, err := p.Capacity(ctx, &v1.GetCapacityRequest{Range: rg})
	if err != nil {
		t.Fatal(err)
	}
	if facts.Servers[0].PerDay == nil {
		t.Fatal("mature six-hour server rejected after one second of boundary jitter")
	}

}

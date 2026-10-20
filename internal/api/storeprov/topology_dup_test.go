package storeprov_test

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// A connection stored twice in one collection, then missing, then back:
// the SQL count agrees with CountTopologyDrops, the duplicate hiding no
// gap. Found by the third review of the Topology API.
func TestTopologyDropsDuplicateRow(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	at0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	var hist []api.TopologyCollection
	stored := map[int64]int{1: 2, 2: 0, 3: 1, 4: 1, 5: 2} // rows of lid 1 per epoch
	for e := int64(1); e <= 5; e++ {
		at := at0.Add(time.Duration(e) * 30 * time.Second)
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 1, 1, ['h'], ['h'], [], 0, false, [], NULL)`, e, at, at); err != nil {
			t.Fatal(err)
		}
		r.add("epoch_endpoints", map[string]any{"epoch": e, "server_id": "H", "endpoint": "LEAFZ", "state": "ok"})
		c := api.TopologyCollection{Epoch: e, At: at, Leafz: map[string]bool{"H": true}, Gatewayz: map[string]bool{}}
		for range stored[e] {
			r.add("leafnodes", map[string]any{"epoch": e, "server_id": "H", "lid": 1, "name": "site", "account": "A", "is_spoke": false})
			c.Leafnodes = append(c.Leafnodes, api.TopologyLeafRow{ServerID: "H", Server: "h", Cluster: "c", LID: 1, Name: "site", Account: "A"})
		}
		hist = append(hist, c)
	}
	r.add("servers", map[string]any{"server_id": "H", "server_name": "h", "cluster": "c", "first_epoch": 1, "last_epoch": 5,
		"first_seen_at": at0, "last_seen_at": at0})
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second}
	for _, e := range []int{3, 5} {
		E := hist[e-1]
		f, err := p.Topology(ctx, &v1.GetTopologyRequest{Range: &v1.TimeRange{Start: timestamppb.New(E.At), End: timestamppb.New(E.At.Add(time.Microsecond))}})
		if err != nil {
			t.Fatal(err)
		}
		want := api.CountTopologyDrops(hist[:e], 30*time.Second, E.Epoch, E.At)
		if len(f.Drops.Leafs) != 1 || len(want.Leafs) != 1 || f.Drops.Leafs[0] != want.Leafs[0] {
			t.Errorf("epoch %d: SQL %+v, Go %+v", e, f.Drops.Leafs, want.Leafs)
		}
	}
}

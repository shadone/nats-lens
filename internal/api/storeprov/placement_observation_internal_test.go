package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
	"google.golang.org/protobuf/proto"
)

func observedSamples(state v1.PlacementState) []*placementSample {
	out := []*placementSample{}
	for e := 3; e >= 1; e-- {
		s := &placementSample{epoch: int64(e), at: time.Date(2026, 10, 17, 12, 0, e*30, 0, time.UTC), reps: map[string]*replicaReport{}, cells: map[string]*v1.PlacementCell{}, identities: map[string]string{"nats-0": "S0/created", "nats-1": "S1/created"}, peers: map[string][]string{"nats-0": {"nats-1"}}, selectedID: "S1"}
		s.reps["nats-0"] = &replicaReport{reports: true, leader: true, hash: "h", byLeader: map[string]claimantView{}}
		s.reps["nats-1"] = &replicaReport{reports: state != v1.PlacementState_PLACEMENT_STATE_MISSING, hash: "h", byLeader: map[string]claimantView{"nats-0": {current: no, offline: no, lag: sql.NullInt64{Valid: true, Int64: 0}}}}
		switch state {
		case v1.PlacementState_PLACEMENT_STATE_OFFLINE:
			s.reps["nats-1"].byLeader["nats-0"] = claimantView{offline: yes}
		case v1.PlacementState_PLACEMENT_STATE_DISAGREES:
			s.reps["nats-1"].byLeader = map[string]claimantView{}
		}
		out = append(out, s)
	}
	return out
}

func TestObservedPlacementContinuity(t *testing.T) {
	for _, state := range []v1.PlacementState{v1.PlacementState_PLACEMENT_STATE_BEHIND, v1.PlacementState_PLACEMENT_STATE_MISSING, v1.PlacementState_PLACEMENT_STATE_OFFLINE, v1.PlacementState_PLACEMENT_STATE_DISAGREES} {
		t.Run(state.String(), func(t *testing.T) {
			for _, tc := range []struct {
				name, boundary string
				edit           func(*placementSample)
			}{
				{"matching", "range_limit", func(*placementSample) {}},
				{"unknown evidence", "evidence_unknown", func(s *placementSample) { s.unknown = true }},
				{"no cell", "evidence_unknown", func(s *placementSample) { delete(s.reps, "nats-1") }},
				{"collection gap", "collection_gap", func(s *placementSample) { s.at = s.at.Add(-2 * time.Minute) }},
				{"epoch gap", "collection_gap", func(s *placementSample) { s.epoch = 0 }},
				{"process restart", "identity_changed", func(s *placementSample) { s.selectedID = "old" }},
				{"incarnation", "identity_changed", func(s *placementSample) { s.identities["nats-0"] = "S0/other-created" }},
				{"membership", "identity_changed", func(s *placementSample) { s.peers["nats-0"] = []string{"nats-1", "nats-2"} }},
				{"state changed", "state_changed", func(s *placementSample) {
					s.reps["nats-1"] = &replicaReport{reports: true, hash: "h", byLeader: map[string]claimantView{"nats-0": {current: yes}}}
				}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					ss := observedSamples(state)
					tc.edit(ss[1])
					o := observePlacement(ss, "APP/orders", "nats-1", "range_limit", 30*time.Second)
					if o == nil {
						t.Fatal("no observation")
					}
					want := int32(1)
					if tc.name == "matching" {
						want = 3
					}
					if o.Samples != want || o.Boundary != tc.boundary || o.Truncated != (want == 3) {
						t.Fatalf("%+v", o)
					}
					if want == 1 && !proto.Equal(o.AsOf, o.Since) {
						t.Fatal("one sample must have zero span")
					}
				})
			}
		})
	}
}

func TestObservedPlacementDisagreementReasons(t *testing.T) {
	ss := observedSamples(v1.PlacementState_PLACEMENT_STATE_DISAGREES)
	// Same classifier DISAGREES, first unlisted, then listed but hash differs.
	ss[1].reps["nats-1"].byLeader["nats-0"] = claimantView{current: yes}
	ss[1].reps["nats-1"].hash = "other"
	ss[1].cells["nats-1"] = &v1.PlacementCell{ConfigHash: "other"}
	o := observePlacement(ss, "APP/orders", "nats-1", "range_limit", 30*time.Second)
	if o.Samples != 1 {
		t.Fatalf("bridged disagreement reason change: %v", o)
	}
	// No leader and a single hash is unknown, not an observed problem duration.
	ss = observedSamples(v1.PlacementState_PLACEMENT_STATE_BEHIND)
	ss[0].reps["nats-0"].leader = false
	if o = observePlacement(ss, "APP/orders", "nats-1", "range_limit", 30*time.Second); o != nil {
		t.Fatalf("unknown state got duration: %v", o)
	}
}

// Synthetic 300 collections x 100 unrelated assets. Four history reads,
// independent of collection and asset counts; collection cap is explicit.
func TestPlacementObservationBoundedBatch(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db := st.DB()
	at := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	queries := []string{
		`INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) VALUES ('S0','nats-0',1,300,?,?),('S1','nats-1',1,300,?,?)`,
	}
	if _, err = db.Exec(queries[0], at, at, at, at); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO epochs SELECT i, make_timestamp(?::BIGINT)+i*INTERVAL 30 SECOND, make_timestamp(?::BIGINT)+i*INTERVAL 30 SECOND,0,2,2,[],[],[],0,false,[],NULL FROM range(1,301) r(i)`,
	} {
		if _, err = db.Exec(q, at.UnixMicro(), at.UnixMicro()); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		`INSERT INTO stream_config(server_id,account,stream,first_epoch,last_epoch,replicas,config_hash) SELECT id,'APP','orders',1,300,2,'h' FROM (VALUES ('S0'),('S1')) t(id)`,
		`INSERT INTO epoch_endpoints SELECT i,id,'JSZ','ok',NULL FROM range(1,301) r(i) CROSS JOIN (VALUES ('S0'),('S1')) t(id)`,
		`INSERT INTO stream_stats(epoch,server_id,account,stream,is_leader,created) SELECT i,id,'APP',CASE WHEN a=0 THEN 'orders' ELSE 'unrelated-'||a END,id='S0',TIMESTAMP '2026-10-01' FROM range(1,301) r(i) CROSS JOIN range(0,101) x(a) CROSS JOIN (VALUES ('S0'),('S1')) t(id)`,
		`INSERT INTO stream_replicas(epoch,server_id,account,stream,peer_name,current,offline,lag) SELECT i,'S0','APP','orders','nats-1',false,false,0 FROM range(1,301) r(i)`,
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	spy := &countedOnCallDB{DB: db}
	p := &Provider{St: st, Interval: 30 * time.Second, readDB: spy}
	req := &v1.GetPlacementRequest{SelectedAsset: "APP/orders", SelectedServer: "nats-1", Range: &v1.TimeRange{Start: ts(at), End: ts(at.Add(4 * time.Hour))}}
	end := epochInfo{epoch: 300, startedAt: at.Add(300 * 30 * time.Second)}
	o, err := p.placementObservation(ctx, req, end)
	if err != nil {
		t.Fatal(err)
	}
	if spy.queries != 4 || o.Samples != 256 || !o.Truncated || o.Boundary != "collection_limit" {
		t.Fatalf("queries=%d observation=%v", spy.queries, o)
	}
	// Same asset in another account has no evidence; past as-of excludes future rows.
	req.SelectedAsset = "OTHER/orders"
	spy.queries = 0
	if o, err = p.placementObservation(ctx, req, end); err != nil || o != nil {
		t.Fatalf("account isolation %v %v", o, err)
	}
	req.SelectedAsset = "APP/orders"
	end = epochInfo{epoch: 2, startedAt: at.Add(time.Minute)}
	o, err = p.placementObservation(ctx, req, end)
	if err != nil || o.Samples != 2 || !o.AsOf.AsTime().Equal(end.startedAt) {
		t.Fatalf("as-of isolation %v %v", o, err)
	}
	t.Log(fmt.Sprintf("4 queries, 256 samples capped from 300 collections with 60,600 stats rows; zero lag preserved as behind when current=false"))
}

func TestObservedDisagreementReferenceChanges(t *testing.T) {
	ss := observedSamples(v1.PlacementState_PLACEMENT_STATE_DISAGREES)
	for _, s := range ss {
		s.reps["nats-1"].hash = "different"
		s.cells["nats-0"] = &v1.PlacementCell{ConfigHash: "h"}
		s.cells["nats-1"] = &v1.PlacementCell{ConfigHash: "different"}
	}
	ss[1].reps["nats-0"].hash = "new-reference"
	ss[1].cells["nats-0"].ConfigHash = "new-reference"
	o := observePlacement(ss, "APP/orders", "nats-1", "range_limit", 30*time.Second)
	if o.Samples != 1 || o.Boundary != "identity_changed" {
		t.Fatalf("reference change bridged: %v", o)
	}
}

type failedObservationDB struct {
	queryReader
	at, calls int
}

var observationFailure = fmt.Errorf("synthetic history read failure")

func (d *failedObservationDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	d.calls++
	if d.calls == d.at {
		return nil, observationFailure
	}
	return d.queryReader.QueryContext(ctx, q, args...)
}
func TestPlacementObservationReadFailures(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	if _, err = st.DB().Exec(`INSERT INTO epochs VALUES(1,?,?,0,1,1,[],[],[],0,false,[],NULL)`, at, at); err != nil {
		t.Fatal(err)
	}
	req := &v1.GetPlacementRequest{SelectedAsset: "APP/orders", SelectedServer: "nats-1", Range: &v1.TimeRange{Start: ts(at.Add(-time.Hour)), End: ts(at.Add(time.Minute))}}
	for i := 1; i <= 4; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			db := &failedObservationDB{queryReader: st.DB(), at: i}
			p := &Provider{St: st, readDB: db}
			o, err := p.placementObservation(ctx, req, epochInfo{epoch: 1, startedAt: at})
			if err != observationFailure || o != nil {
				t.Fatalf("failure %d: %v %v", i, o, err)
			}
		})
	}
}

package storeprov_test

import (
	"slices"
	"strconv"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// replicaRow is one server's report of a stream, and what the leader says
// of that server's replica.
type replicaRow struct {
	id, name string
	reports  bool   // the server has a stream_stats row
	leader   bool   // the server leads the stream
	hash     string // its config hash
	current  bool   // the leader reports the replica current
	offline  bool   // the leader reports the replica offline
	lag      int64
}

// placementStore writes epoch 1 with order-jobs held by the given replicas,
// configured with replicas copies.
func placementStore(t *testing.T, replicas int32, reps []replicaRow) *store.Store {
	t.Helper()
	return placementStoreFor(t, "order-jobs", replicas, reps)
}

// placementStoreFor is placementStore for another stream.
func placementStoreFor(t *testing.T, stream string, replicas int32, reps []replicaRow) *store.Store {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	var ids []string
	for _, x := range reps {
		r.server(x.id, x.name)
		if x.reports {
			ids = append(ids, x.id)
		}
	}
	r.epoch(1, t0, ids...)
	r.add("streams", map[string]any{"account": "APP", "stream": stream, "first_epoch": 1, "last_epoch": 1})
	var lead string
	for _, x := range reps {
		if x.leader {
			lead = x.id
		}
	}
	for _, x := range reps {
		if !x.reports {
			continue
		}
		r.add("stream_stats", map[string]any{"epoch": 1, "server_id": x.id, "account": "APP", "stream": stream,
			"is_leader": x.leader, "messages": 10, "bytes": 100})
		r.add("stream_config", map[string]any{"server_id": x.id, "account": "APP", "stream": stream,
			"first_epoch": 1, "last_epoch": 1, "config_hash": x.hash, "replicas": replicas})
	}
	for _, x := range reps {
		if x.leader || lead == "" {
			continue
		}
		r.add("stream_replicas", map[string]any{"epoch": 1, "server_id": lead, "account": "APP", "stream": stream,
			"peer_name": x.name, "current": x.current, "offline": x.offline, "lag": x.lag})
	}
	return st
}

func streamPlacementOf(t *testing.T, st *store.Store) []*v1.ReplicaPlacement {
	t.Helper()
	c := serveSeries(t, st, t0.Add(30*time.Second))
	r := &v1.TimeRange{Start: timestamppb.New(t0.Add(-time.Hour)), End: timestamppb.New(t0.Add(30 * time.Second))}
	ss := must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Range: r}))).GetStreams()
	if len(ss) != 1 {
		t.Fatalf("streams = %v, want order-jobs", ss)
	}
	return ss[0].GetPlacement()
}

func states(ps []*v1.ReplicaPlacement) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.GetServer()+"="+p.GetState().String())
	}
	return out
}

// A follower the leader reports behind is BEHIND; one current is IN_SYNC.
// The squares are in server name order.
func TestPlacementLaggingFollower(t *testing.T) {
	st := placementStore(t, 3, []replicaRow{
		{id: "S0", name: "nats-0", reports: true, leader: true, hash: "h"},
		{id: "S1", name: "nats-1", reports: true, hash: "h", current: true},
		{id: "S2", name: "nats-2", reports: true, hash: "h", lag: 5000},
	})
	want := []string{"nats-0=PLACEMENT_STATE_LEADER", "nats-1=PLACEMENT_STATE_IN_SYNC", "nats-2=PLACEMENT_STATE_BEHIND"}
	if got := states(streamPlacementOf(t, st)); !slices.Equal(got, want) {
		t.Fatalf("placement = %v, want %v", got, want)
	}
}

// A follower with another config hash disagrees; a peer the leader reports
// offline is OFFLINE; a peer the leader names that reports nothing itself
// is MISSING; a configured replica no server names is MISSING with no
// server.
func TestPlacementDisagreesOfflineMissing(t *testing.T) {
	st := placementStore(t, 5, []replicaRow{
		{id: "S0", name: "nats-0", reports: true, leader: true, hash: "h"},
		{id: "S1", name: "nats-1", reports: true, hash: "other", current: true},
		{id: "S2", name: "nats-2", offline: true},
		{id: "S3", name: "nats-3", current: true},
	})
	want := []string{"nats-0=PLACEMENT_STATE_LEADER", "nats-1=PLACEMENT_STATE_DISAGREES", "nats-2=PLACEMENT_STATE_OFFLINE",
		"nats-3=PLACEMENT_STATE_MISSING", "=PLACEMENT_STATE_MISSING"}
	if got := states(streamPlacementOf(t, st)); !slices.Equal(got, want) {
		t.Fatalf("placement = %v, want %v", got, want)
	}
}

// Without a leader row nothing says how a follower stands: its state is
// unset, not in sync.
func TestPlacementWithoutLeaderIsUnspecified(t *testing.T) {
	st := placementStore(t, 2, []replicaRow{
		{id: "S0", name: "nats-0", reports: true, hash: "h"},
		{id: "S1", name: "nats-1", reports: true, hash: "h"},
	})
	want := []string{"nats-0=PLACEMENT_STATE_UNSPECIFIED", "nats-1=PLACEMENT_STATE_UNSPECIFIED"}
	if got := states(streamPlacementOf(t, st)); !slices.Equal(got, want) {
		t.Fatalf("placement = %v, want %v", got, want)
	}
}

// A KV bucket gets the same placement as a stream, padded with MISSING up
// to its configured replicas.
func TestBucketPlacementPadsToReplicas(t *testing.T) {
	st := placementStoreFor(t, "KV_sessions", 3, []replicaRow{
		{id: "S0", name: "nats-0", reports: true, leader: true, hash: "h"},
		{id: "S1", name: "nats-1", reports: true, hash: "h", current: true},
	})
	c := serveSeries(t, st, t0.Add(30*time.Second))
	r := &v1.TimeRange{Start: timestamppb.New(t0.Add(-time.Hour)), End: timestamppb.New(t0.Add(30 * time.Second))}
	kvs := must(c.ListKeyValueStores(ctx, connect.NewRequest(&v1.ListKeyValueStoresRequest{Range: r}))).GetKeyValueStores()
	if len(kvs) != 1 {
		t.Fatalf("buckets = %v", kvs)
	}
	want := []string{"nats-0=PLACEMENT_STATE_LEADER", "nats-1=PLACEMENT_STATE_IN_SYNC", "=PLACEMENT_STATE_MISSING"}
	if got := states(kvs[0].GetPlacement()); !slices.Equal(got, want) {
		t.Fatalf("placement = %v, want %v", got, want)
	}
}

// Two servers claim the lead; nats-0 wins by name, so only its replica
// list counts: it reports nats-1 behind while nats-2's stale list says
// current. Whichever list the store holds first, nats-1 is BEHIND, and
// nats-2, which the winner does not list, DISAGREES.
func TestPlacementUsesTheChosenLeadersList(t *testing.T) {
	type list struct {
		lead    string
		current bool
		lag     int64
	}
	winner, loser := list{"S0", false, 9}, list{"S2", true, 0}
	for _, order := range [][]list{{winner, loser}, {loser, winner}} {
		st := emptyStore(t)
		r := rows{t, st}
		ids := []string{"S0", "S1", "S2"}
		for i, id := range ids {
			r.server(id, "nats-"+strconv.Itoa(i))
		}
		r.epoch(1, t0, ids...)
		r.add("streams", map[string]any{"account": "APP", "stream": "order-jobs", "first_epoch": 1, "last_epoch": 1})
		for _, id := range ids {
			r.add("stream_stats", map[string]any{"epoch": 1, "server_id": id, "account": "APP", "stream": "order-jobs",
				"is_leader": id != "S1", "messages": 10, "bytes": 100})
			r.add("stream_config", map[string]any{"server_id": id, "account": "APP", "stream": "order-jobs",
				"first_epoch": 1, "last_epoch": 1, "config_hash": "h", "replicas": 3})
		}
		for _, l := range order {
			r.add("stream_replicas", map[string]any{"epoch": 1, "server_id": l.lead, "account": "APP", "stream": "order-jobs",
				"peer_name": "nats-1", "current": l.current, "offline": false, "lag": l.lag})
		}
		want := []string{"nats-0=PLACEMENT_STATE_LEADER", "nats-1=PLACEMENT_STATE_BEHIND", "nats-2=PLACEMENT_STATE_DISAGREES"}
		if got := states(streamPlacementOf(t, st)); !slices.Equal(got, want) {
			t.Errorf("lists in order %v: placement = %v, want %v", order, got, want)
		}
	}
}

package storeprov_test

import (
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func laneKinds(evs []*v1.LaneEvent) []string {
	var out []string
	for _, e := range evs {
		out = append(out, e.GetKind())
	}
	return out
}

// The replay fixture's restarts, lame duck, leader election and client
// events land on the server, leader and client lanes and leave the NATS
// lane, with the session in a time zone other than UTC.
func TestTimelineSubjectLanes(t *testing.T) {
	t.Setenv("TZ", "Pacific/Honolulu")
	p, r := replayStore(t)
	if _, err := p.St.DB().Exec(`SET GLOBAL TimeZone = 'Pacific/Honolulu'`); err != nil {
		t.Fatal(err)
	}
	r.add("events", map[string]any{"id": 900, "time": rEpoch(13).Add(2 * time.Second), "received_at": rEpoch(13), "source": "sys",
		"kind": "client.churn", "client_name": "orders-api", "summary": "churn", "raw": `{"connects":4}`})
	r.add("events", map[string]any{"id": 901, "time": rEpoch(13).Add(3 * time.Second), "received_at": rEpoch(13), "source": "sys",
		"kind": "client.disconnect", "client_name": "orders-api", "reason": "Client Closed", "summary": "closed"})
	_, c := apitest.Serve(t, p)
	req := &v1.GetStatusTimelineRequest{Range: &v1.TimeRange{Start: timestamppb.New(rT), End: timestamppb.New(rEpoch(50))},
		Bucket: durationpb.New(5 * time.Minute)}
	res := must(c.GetStatusTimeline(ctx, connect.NewRequest(req)))

	if got := laneKinds(res.GetServers()); !slices.Equal(got, []string{"server.lameduck", "server.restarted", "server.restarted"}) {
		t.Fatalf("servers = %v", got)
	}
	restart := res.GetServers()[1]
	if restart.GetLabel() != "nats-1 restarted, version 2.11.8 to 2.12.2" || restart.GetEventId() != "" ||
		!restart.GetTime().AsTime().Equal(rEpoch(11).Add(10*time.Second)) || restart.GetEntity().GetKey() != "nats-1" {
		t.Errorf("restart = %v", restart)
	}
	if got := res.GetServers()[2].GetLabel(); got != "nats-2 restarted, version 2.12.2 unchanged" {
		t.Errorf("second restart = %q", got)
	}
	if got := laneKinds(res.GetLeaders()); !slices.Equal(got, []string{"stream.leader_elected"}) {
		t.Errorf("leaders = %v", got)
	}
	for _, e := range res.GetNats() {
		if e.GetKind() == "server.lameduck" || e.GetKind() == "stream.leader_elected" {
			t.Errorf("%s drawn on the NATS lane too", e.GetKind())
		}
	}
	// Epoch 13 is 6 minutes after rT: bucket 1 of 5 minutes.
	want := &v1.ClientLaneBucket{Index: 1, Connects: 6, Closes: 5, Drops: 1, Shutdowns: 1}
	if len(res.GetClients()) != 1 || res.GetClients()[0].String() != want.String() {
		t.Errorf("clients = %v, want %v", res.GetClients(), want)
	}
	if !res.GetSystemEventsCaptured() || !res.GetJetstreamEventsCaptured() || res.GetServersTruncated() || res.GetLeadersTruncated() {
		t.Errorf("flags: system %v jetstream %v servers cut %v leaders cut %v", res.GetSystemEventsCaptured(), res.GetJetstreamEventsCaptured(),
			res.GetServersTruncated(), res.GetLeadersTruncated())
	}

	// A server's timeline holds its own restarts only; a stream's none.
	req.Entity = &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-2"}
	if got := must(c.GetStatusTimeline(ctx, connect.NewRequest(req))).GetServers(); len(got) != 1 || got[0].GetLabel() != "nats-2 restarted, version 2.12.2 unchanged" {
		t.Errorf("nats-2 servers = %v", got)
	}

	// The event boundary before any event: nothing captured, restarts stay.
	req.Entity, req.EventReadId = nil, new(int64)
	res = must(c.GetStatusTimeline(ctx, connect.NewRequest(req)))
	if res.GetSystemEventsCaptured() || res.GetJetstreamEventsCaptured() || len(res.GetClients()) != 0 || len(res.GetLeaders()) != 0 ||
		!slices.Equal(laneKinds(res.GetServers()), []string{"server.restarted", "server.restarted"}) {
		t.Errorf("boundary 0: system %v jetstream %v clients %v leaders %v servers %v", res.GetSystemEventsCaptured(),
			res.GetJetstreamEventsCaptured(), res.GetClients(), res.GetLeaders(), laneKinds(res.GetServers()))
	}
}

// A range with more leader elections than the lane holds keeps the newest
// 1000 and says it was cut; the server lane, with fewer, is not.
func TestTimelineLeaderLaneTruncated(t *testing.T) {
	p, r := replayStore(t)
	for i := range 1001 {
		at := rEpoch(30).Add(time.Duration(i) * time.Millisecond)
		r.add("events", map[string]any{"id": 2000 + i, "time": at, "received_at": at, "source": "jetstream",
			"kind": "consumer.leader_elected", "summary": "elected"})
	}
	_, c := apitest.Serve(t, p)
	res := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{
		Range: &v1.TimeRange{Start: timestamppb.New(rT), End: timestamppb.New(rEpoch(50))}, Bucket: durationpb.New(5 * time.Minute)})))
	if len(res.GetLeaders()) != 1000 || !res.GetLeadersTruncated() || res.GetServersTruncated() {
		t.Fatalf("leaders %d cut %v, servers cut %v", len(res.GetLeaders()), res.GetLeadersTruncated(), res.GetServersTruncated())
	}
	if last := res.GetLeaders()[999]; last.GetEventId() != "3000" {
		t.Errorf("newest kept = %s, want 3000", last.GetEventId())
	}
}

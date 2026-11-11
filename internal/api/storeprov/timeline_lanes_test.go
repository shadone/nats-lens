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

	// An entity's timeline has none of the subject lanes.
	req.Entity = &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-2"}
	if got := must(c.GetStatusTimeline(ctx, connect.NewRequest(req))); len(got.GetServers())+len(got.GetLeaders())+len(got.GetClients()) != 0 || got.GetSystemEventsCaptured() {
		t.Errorf("nats-2 timeline has subject lanes: %v %v %v", got.GetServers(), got.GetLeaders(), got.GetClients())
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

// Client events count in the bucket of their time, start included and end
// excluded, up to the event boundary; a disconnect without a reason counts
// apart from the drops.
func TestTimelineClientBuckets(t *testing.T) {
	t.Setenv("TZ", "Pacific/Honolulu")
	p, r := replayStore(t)
	if _, err := p.St.DB().Exec(`SET GLOBAL TimeZone = 'Pacific/Honolulu'`); err != nil {
		t.Fatal(err)
	}
	start := rT.Add(2 * time.Hour)
	ev := func(id int, at time.Time, kind, reason string, raw string) {
		row := map[string]any{"id": id, "time": at, "received_at": at, "source": "sys", "kind": kind, "summary": kind, "reason": reason}
		if raw != "" {
			row["raw"] = raw
		}
		r.add("events", row)
	}
	ev(5001, start, "client.connect", "", "")
	ev(5002, start.Add(5*time.Minute), "client.connect", "", "")
	ev(5003, start.Add(5*time.Minute-time.Microsecond), "client.connect", "", "")
	ev(5004, start.Add(11*time.Minute), "client.disconnect", "Read Error", "")
	ev(5005, start.Add(12*time.Minute), "client.connect", "", "")
	ev(5006, start.Add(-time.Second), "client.churn", "Client Closed", `{"connects":7}`)
	ev(5007, start.Add(time.Minute), "client.auth_error", "", "")
	ev(5008, start.Add(11*time.Minute), "client.disconnect", "", "")
	_, c := apitest.Serve(t, p)
	req := &v1.GetStatusTimelineRequest{Range: &v1.TimeRange{Start: timestamppb.New(start), End: timestamppb.New(start.Add(12 * time.Minute))},
		Bucket: durationpb.New(5 * time.Minute)}
	res := must(c.GetStatusTimeline(ctx, connect.NewRequest(req)))
	got := map[int32]*v1.ClientLaneBucket{}
	for _, b := range res.GetClients() {
		got[b.GetIndex()] = b
	}
	if len(got) != 3 || got[0].GetConnects() != 2 || got[0].GetAuthErrors() != 1 || got[1].GetConnects() != 1 ||
		got[2].GetDrops() != 1 || got[2].GetUnknownDisconnects() != 1 {
		t.Errorf("clients = %v", res.GetClients())
	}
	id := int64(5003)
	req.EventReadId = &id
	for _, b := range must(c.GetStatusTimeline(ctx, connect.NewRequest(req))).GetClients() {
		if b.GetDrops() != 0 || b.GetAuthErrors() != 0 || b.GetUnknownDisconnects() != 0 {
			t.Errorf("boundary 5003: %v", b)
		}
	}
}

// A restart that started in the range but was first collected after it
// is not drawn yet.
func TestTimelineRestartFirstCollected(t *testing.T) {
	p, _ := replayStore(t)
	_, c := apitest.Serve(t, p)
	// nats-1's new process started at epoch 11 + 10 s, first collected at
	// epoch 12.
	for end, want := range map[time.Time]int{rEpoch(11).Add(20 * time.Second): 0, rEpoch(12): 1} {
		res := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{
			Range: &v1.TimeRange{Start: timestamppb.New(rT), End: timestamppb.New(end)}, Bucket: durationpb.New(time.Minute)})))
		n := 0
		for _, e := range res.GetServers() {
			if e.GetKind() == "server.restarted" {
				n++
			}
		}
		if n != want {
			t.Errorf("range to %v: %d restarts, want %d", end, n, want)
		}
	}
}

// A collector gap over the whole range: the lanes say capture stopped,
// not that the range was quiet. A lost events connection that did not
// come back is a gap up to the range end.
func TestTimelineCaptureGaps(t *testing.T) {
	t.Setenv("TZ", "Pacific/Honolulu")
	p, r := replayStore(t)
	if _, err := p.St.DB().Exec(`SET GLOBAL TimeZone = 'Pacific/Honolulu'`); err != nil {
		t.Fatal(err)
	}
	start := rT.Add(2 * time.Hour)
	r.add("events", map[string]any{"id": 6001, "time": start.Add(-time.Minute), "received_at": start.Add(2 * time.Hour), "source": "collector",
		"kind": "collector.gap", "summary": "gap"})
	_, c := apitest.Serve(t, p)
	whole := &v1.GetStatusTimelineRequest{Range: &v1.TimeRange{Start: timestamppb.New(start), End: timestamppb.New(start.Add(time.Hour))}, Bucket: durationpb.New(5 * time.Minute)}
	res := must(c.GetStatusTimeline(ctx, connect.NewRequest(whole)))
	if res.GetSystemEventsCaptured() || res.GetJetstreamEventsCaptured() || len(res.GetEventCaptureGaps()) != 1 ||
		!res.GetEventCaptureGaps()[0].GetStart().AsTime().Equal(start) || !res.GetEventCaptureGaps()[0].GetEnd().AsTime().Equal(start.Add(time.Hour)) {
		t.Errorf("whole range in a gap: system %v jetstream %v gaps %v", res.GetSystemEventsCaptured(), res.GetJetstreamEventsCaptured(), res.GetEventCaptureGaps())
	}

	later := start.Add(3 * time.Hour)
	r.add("events", map[string]any{"id": 6002, "time": later.Add(10 * time.Minute), "received_at": later.Add(10 * time.Minute), "source": "collector",
		"kind": "collector.connection_closed", "summary": "closed", "raw": `{"connection":"sys"}`})
	res = must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{
		Range: &v1.TimeRange{Start: timestamppb.New(later), End: timestamppb.New(later.Add(time.Hour))}, Bucket: durationpb.New(5 * time.Minute)})))
	if g := res.GetEventCaptureGaps(); len(g) != 1 || !g[0].GetStart().AsTime().Equal(later.Add(10*time.Minute)) || !g[0].GetEnd().AsTime().Equal(later.Add(time.Hour)) ||
		!res.GetSystemEventsCaptured() {
		t.Errorf("open connection gap: %v, system captured %v", g, res.GetSystemEventsCaptured())
	}
}

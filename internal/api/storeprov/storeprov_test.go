package storeprov_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
	"github.com/shadone/nats-lens/internal/store"
)

var ctx = context.Background()

// now is 30 seconds after the third epoch.
var now = t0.Add(90 * time.Second)

func client(t *testing.T) natslensv1connect.LensServiceClient {
	t.Helper()
	_, c := serve(t, fixtureStore(t))
	return c
}

// serve serves a provider over st at the fixed now.
func serve(t *testing.T, st *store.Store) (*httptest.Server, natslensv1connect.LensServiceClient) {
	t.Helper()
	return apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return now }})
}

// must returns the message of a call that has to succeed; a failed call
// panics, which fails the test with the error.
func must[T any](res *connect.Response[T], err error) *T {
	if err != nil {
		panic(err)
	}
	return res.Msg
}

func codeOf(err error) connect.Code {
	var ce *connect.Error
	if errors.As(err, &ce) {
		return ce.Code()
	}
	return connect.CodeUnknown
}

func hour() *v1.TimeRange { return &v1.TimeRange{Start: timestamppb.New(now.Add(-time.Hour))} }

func TestOverviewFromSnapshots(t *testing.T) {
	o := must(client(t).GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: hour(), MaxPoints: 120})))
	if o.GetCluster() != "example-dev" || o.GetFreshness().GetEpoch() != 3 || !o.GetFreshness().GetPartial() ||
		o.GetFreshness().GetAge().AsDuration() != 30*time.Second {
		t.Fatalf("cluster %q, freshness %v", o.GetCluster(), o.GetFreshness())
	}
	if len(o.GetServers()) != 2 || o.GetWorstFinding() != nil || o.GetTally().GetCritical() != 0 {
		t.Fatalf("servers %d, worst %v", len(o.GetServers()), o.GetWorstFinding())
	}
	tiles := map[string]*v1.Tile{}
	for _, tile := range o.GetTiles() {
		tiles[tile.GetId()] = tile
	}
	if got := tiles["servers"]; got.GetValue() != 2 || got.GetValueSuffix() != "/ 3" || got.GetSubtitle() != "v2.15.0 on all · up 0 min" {
		t.Fatalf("servers tile %v", got)
	}
	if got := tiles["clients"].GetValue(); got != 4 {
		t.Fatalf("clients = %v", got)
	}
	if got := tiles["clients"].GetSubtitle(); got != "2 apps · 3 slow" {
		t.Fatalf("clients subtitle = %q, want 2 apps and 3 slow consumer events (nats-0 0, 1, 2 and the old nats-1 0, 1; the new process starts a new count)", got)
	}
	if got := tiles["consumers"]; got.GetValue() != 5 || got.Value2 != nil || got.GetSubtitle() != "0 behind" {
		t.Fatalf("consumers tile = %v", got)
	}
	// Growth needs six hours of data; the fixture holds a minute.
	if got := tiles["storage"]; got.GetLimit() != 100<<30 || got.GetValueSuffix() != "of 100 GiB" || got.GetSubtitle() != "0.5% used" {
		t.Fatalf("storage tile = %v", got)
	}
}

func TestServersFromSnapshots(t *testing.T) {
	c := client(t)
	sv := must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{Range: hour()})))
	if len(sv.GetServers()) != 2 || strings.Join(sv.GetMissing(), ",") != "nats-2" {
		t.Fatalf("servers = %v, missing %v", sv.GetServers(), sv.GetMissing())
	}
	n1 := sv.GetServers()[1]
	if n1.GetName() != "nats-1" || n1.GetServerId() != "NFAKESERVER1NEW" || len(n1.GetRestarts()) != 1 || !n1.GetPresent() ||
		n1.GetHealthz() != "ok" || n1.GetUptime().AsDuration() != 10*time.Second || n1.GetStatus() != v1.Status_STATUS_NO_DATA {
		// The fixture's checks never ran, so nothing says the server is ok.
		t.Fatalf("nats-1 = %v", n1)
	}
	if sv.GetServers()[0].GetRole() == v1.ServerRole_SERVER_ROLE_UNSPECIFIED {
		t.Fatalf("nats-0 role unset: %v", sv.GetServers()[0])
	}
	gs := must(c.GetServer(ctx, connect.NewRequest(&v1.GetServerRequest{Name: "nats-1", Range: hour()})))
	if len(gs.GetProcesses()) != 2 || len(gs.GetEndpoints()) == 0 {
		t.Fatalf("nats-1 detail = %v", gs)
	}
	if _, err := c.GetServer(ctx, connect.NewRequest(&v1.GetServerRequest{Name: "nats-2", Range: hour()})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("nats-2 never answered: %v", err)
	}
	cs := must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{Range: hour(), PageSize: 3})))
	if cs.GetTotalCount() != 4 || len(cs.GetConnections()) != 3 || cs.GetNextPageToken() == "" || cs.GetEpoch() != 3 {
		t.Fatalf("connections page = %v", cs)
	}
	rest := must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{Range: hour(), PageSize: 3, PageToken: cs.GetNextPageToken()})))
	if len(rest.GetConnections()) != 1 || rest.GetNextPageToken() != "" || rest.GetConnections()[0].GetServer() != "nats-1" {
		t.Fatalf("second page = %v", rest)
	}
	f := must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{Range: hour(), Server: "nats-0", NameContains: "ledger"})))
	if f.GetTotalCount() != 1 || f.GetConnections()[0].GetRtt().AsDuration() != 2*time.Millisecond {
		t.Fatalf("filtered = %v", f)
	}
}

func TestStreamsAndConsumersFromSnapshots(t *testing.T) {
	c := client(t)
	st := must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Range: hour()})))
	names := []string{}
	for _, s := range st.GetStreams() {
		names = append(names, s.GetEntity().GetName())
	}
	if strings.Join(names, ",") != "catalog-changes,order-archive,order-jobs,test-stream" {
		t.Fatalf("streams = %v", names)
	}
	if st.GetTotals().GetDurableConsumers() != 5 || st.GetTotals().GetEphemeralConsumers() != 1 {
		t.Fatalf("totals = %v", st.GetTotals())
	}
	jobs := st.GetStreams()[2]
	if jobs.GetReplicas() != 3 || jobs.GetLeader() != "nats-1" || jobs.GetConsumers() != 2 || jobs.GetMessages() == 0 ||
		len(jobs.GetBytes_7D().GetPoints()) == 0 {
		t.Fatalf("order-jobs = %v", jobs)
	}
	if n := len(must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Range: hour(), IncludeKv: true}))).GetStreams()); n != 5 {
		t.Fatalf("with kv: %d", n)
	}
	gs := must(c.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{Account: account, Stream: "order-jobs", Range: hour()})))
	if len(gs.GetServers()) != 2 || len(gs.GetConsumers()) != 2 || len(gs.GetConfig()) == 0 {
		t.Fatalf("stream detail = %v", gs)
	}
	var leaders int
	for _, v := range gs.GetServers() {
		if v.GetIsLeader() {
			leaders++
		}
	}
	if leaders != 1 {
		t.Fatalf("%d leaders in %v", leaders, gs.GetServers())
	}
	if _, err := c.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{Account: account, Stream: "nope", Range: hour()})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown stream: %v", err)
	}
	cs := must(c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{Range: hour()})))
	if len(cs.GetConsumers()) != 5 {
		t.Fatalf("durable consumers = %d", len(cs.GetConsumers()))
	}
	if n := len(must(c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{Range: hour(), IncludeEphemeral: true}))).GetConsumers()); n != 6 {
		t.Fatalf("with ephemeral = %d", n)
	}
	if n := len(must(c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{Range: hour(), Stream: "order-jobs"}))).GetConsumers()); n != 2 {
		t.Fatalf("order-jobs consumers = %d", n)
	}
	gc := must(c.GetConsumer(ctx, connect.NewRequest(&v1.GetConsumerRequest{Account: account, Stream: "order-jobs", Consumer: "index-worker", Range: hour()})))
	if !gc.GetConsumer().GetDurable() || gc.GetConsumer().GetLeader() != "nats-0" || len(gc.GetServers()) != 2 || len(gc.GetConfig()) == 0 {
		t.Fatalf("index-worker = %v", gc)
	}
}

func TestTimeSeriesFromSnapshots(t *testing.T) {
	c := client(t)
	s := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "server.in_msgs_rate", Range: hour(), MaxPoints: 120,
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-0"}}))).GetSeries()
	if len(s.GetPoints()) != 2 || s.GetPoints()[0].GetValue() != 10 || s.GetStep().AsDuration() != 30*time.Second {
		t.Fatalf("nats-0 in rate = %v", s)
	}
	b := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "stream.bytes", Range: hour(), MaxPoints: 1,
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: account + "/order-jobs"}}))).GetSeries()
	if len(b.GetPoints()) != 1 || b.GetPoints()[0].GetValue() <= 0 || b.GetStep().AsDuration() != time.Hour {
		t.Fatalf("one-point bytes = %v", b)
	}
	up := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "cluster.servers_up", Range: hour()}))).GetSeries()
	if len(up.GetPoints()) != 3 || up.GetMin() != 2 || up.GetMax() != 2 {
		t.Fatalf("servers up = %v", up)
	}
	_, err := c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "consumer.num_pending", Range: hour(),
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: account + "/order-jobs/nope"}}))
	if codeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown consumer: %v", err)
	}
}

func TestEventsFromStore(t *testing.T) {
	c := client(t)
	all := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: hour()})))
	if all.GetTotalCount() != 5 || all.GetEvents()[0].GetKind() != "server.shutdown" || all.GetEvents()[0].GetSeverity() != v1.Severity_SEVERITY_CRITICAL {
		t.Fatalf("events = %v", all.GetEvents())
	}
	var ids []int
	token := ""
	for {
		p := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: hour(), PageSize: 2, PageToken: token})))
		for _, e := range p.GetEvents() {
			id, _ := strconv.Atoi(e.GetId())
			ids = append(ids, id)
		}
		if token = p.GetNextPageToken(); token == "" {
			break
		}
	}
	if len(ids) != 5 || ids[0] <= ids[4] {
		t.Fatalf("paged ids = %v", ids)
	}
	filters := map[string]struct {
		req  *v1.ListEventsRequest
		want int32
	}{
		"collector":  {&v1.ListEventsRequest{Sources: []v1.EventSource{v1.EventSource_EVENT_SOURCE_COLLECTOR}}, 1},
		"kubernetes": {&v1.ListEventsRequest{Sources: []v1.EventSource{v1.EventSource_EVENT_SOURCE_KUBERNETES}}, 0},
		"kinds":      {&v1.ListEventsRequest{Kinds: []string{"consumer.deleted", "consumer.created"}}, 2},
		"consumer by account ID": {&v1.ListEventsRequest{Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER,
			Key: account + "/order-jobs/resize-worker"}}, 2},
		"stream":    {&v1.ListEventsRequest{Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: account + "/order-jobs"}}, 2},
		"server":    {&v1.ListEventsRequest{Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-1"}}, 2},
		"client":    {&v1.ListEventsRequest{ClientName: "load-churn"}, 2},
		"too early": {&v1.ListEventsRequest{Range: &v1.TimeRange{Start: timestamppb.New(t0.Add(-time.Hour)), End: timestamppb.New(t0)}}, 0},
	}
	for name, f := range filters {
		if f.req.Range == nil {
			f.req.Range = hour()
		}
		if got := must(c.ListEvents(ctx, connect.NewRequest(f.req))).GetTotalCount(); got != f.want {
			t.Errorf("%s: %d, want %d", name, got, f.want)
		}
	}
	slow := all.GetEvents()[1]
	if slow.GetKind() != "client.disconnect" || slow.GetSeverity() != v1.Severity_SEVERITY_WARNING {
		t.Fatalf("slow consumer disconnect = %v", slow)
	}
	ev := must(c.GetEvent(ctx, connect.NewRequest(&v1.GetEventRequest{Id: slow.GetId()}))).GetEvent()
	if !strings.Contains(ev.GetRaw(), `"id":7`) || ev.GetType() != "CLIENT.DISCONNECT" || ev.GetSource() != v1.EventSource_EVENT_SOURCE_NATS {
		t.Fatalf("event = %v", ev)
	}
	if _, err := c.GetEvent(ctx, connect.NewRequest(&v1.GetEventRequest{Id: "999"})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown event: %v", err)
	}
	tl := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: hour()})))
	if tl.GetBucketCount() != 4 || len(tl.GetRows()) != 0 || len(tl.GetNats()) != 3 {
		t.Fatalf("timeline: %d buckets, %d rows, nats lane %v", tl.GetBucketCount(), len(tl.GetRows()), tl.GetNats())
	}
}

func TestFindingsAndMutesInStore(t *testing.T) {
	c := client(t)
	if n := must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{}))).GetTotalCount(); n != 0 {
		t.Fatalf("findings = %d before the checks exist", n)
	}
	if _, err := c.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{Id: "x"})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("get finding: %v", err)
	}
	e := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: account + "/order-jobs"}
	m := must(c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{CheckCode: "stream-025", Entity: e, Reason: "known",
		MutedBy: "op", ExpiresAt: timestamppb.New(now.Add(time.Hour))}))).GetMute()
	if m.GetId() == "" || m.GetEntity().GetType() != v1.EntityType_ENTITY_TYPE_STREAM || !m.GetCreatedAt().AsTime().Equal(now) {
		t.Fatalf("mute = %v", m)
	}
	whole := must(c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{CheckCode: "server-018", Reason: "noisy",
		MutedBy: "op", ExpiresAt: timestamppb.New(now.Add(time.Minute))}))).GetMute()
	if whole.GetEntity() != nil {
		t.Fatalf("whole-check mute has an entity: %v", whole)
	}
	if got := must(c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{}))).GetMutes(); len(got) != 2 || got[0].GetId() != whole.GetId() {
		t.Fatalf("mutes = %v", got)
	}
	un := must(c.UnmuteFinding(ctx, connect.NewRequest(&v1.UnmuteFindingRequest{MuteId: m.GetId(), UnmutedBy: "op"}))).GetMute()
	if un.GetUnmutedAt() == nil {
		t.Fatalf("unmute = %v", un)
	}
	if _, err := c.UnmuteFinding(ctx, connect.NewRequest(&v1.UnmuteFindingRequest{MuteId: m.GetId(), UnmutedBy: "op"})); codeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("second unmute: %v", err)
	}
	if _, err := c.UnmuteFinding(ctx, connect.NewRequest(&v1.UnmuteFindingRequest{MuteId: "abc", UnmutedBy: "op"})); codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("bad id: %v", err)
	}
	if _, err := c.UnmuteFinding(ctx, connect.NewRequest(&v1.UnmuteFindingRequest{MuteId: "77", UnmutedBy: "op"})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown id: %v", err)
	}
	if got := must(c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{IncludeInactive: true}))).GetMutes(); len(got) != 2 {
		t.Fatalf("all mutes = %d", len(got))
	}
}

func TestEmptyDatabase(t *testing.T) {
	c := client(t)
	past := &v1.TimeRange{Start: timestamppb.New(t0.Add(-48 * time.Hour)), End: timestamppb.New(t0.Add(-24 * time.Hour))}
	o := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: past})))
	if len(o.GetServers()) != 0 || o.GetFreshness().GetEpoch() != 0 {
		t.Fatalf("overview before any epoch = %v", o)
	}
	if n := len(must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Range: past}))).GetStreams()); n != 0 {
		t.Fatalf("streams before any epoch = %d", n)
	}
	if _, err := c.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{Account: account, Stream: "order-jobs", Range: past})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("stream before any epoch: %v", err)
	}
}

package fake_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
	"github.com/shadone/nats-lens/internal/webui"
)

func tsAt(h, m, s int) *timestamppb.Timestamp { return timestamppb.New(at(h, m, s)) }

func TestListEventsPagesNewestFirst(t *testing.T) {
	c := client(t)
	first := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{PageSize: 15})))
	if first.GetTotalCount() != 214 || first.GetEvents()[0].GetType() != "stream-020 Backlog growing" {
		t.Fatalf("total %d, first %v", first.GetTotalCount(), first.GetEvents()[0])
	}
	del := first.GetEvents()[7]
	if del.GetKind() != "consumer.deleted" || len(del.GetRaised()) != 1 || del.GetRaised()[0].GetCheckCode() != "consumer-006" {
		t.Fatalf("eighth event = %v", del)
	}
	seen := map[string]bool{}
	var last *v1.Event
	token := ""
	for {
		p := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{PageSize: 50, PageToken: token})))
		for _, e := range p.GetEvents() {
			if seen[e.GetId()] {
				t.Fatalf("event %s twice", e.GetId())
			}
			seen[e.GetId()] = true
			if last != nil && e.GetTime().AsTime().After(last.GetTime().AsTime()) {
				t.Fatalf("event %s newer than %s", e.GetId(), last.GetId())
			}
			last = e
		}
		if token = p.GetNextPageToken(); token == "" {
			break
		}
	}
	if len(seen) != 214 {
		t.Fatalf("paged %d events", len(seen))
	}
	filters := map[string]struct {
		req  *v1.ListEventsRequest
		want int32
	}{
		"kubernetes":    {&v1.ListEventsRequest{Sources: []v1.EventSource{v1.EventSource_EVENT_SOURCE_KUBERNETES}}, 7},
		"kind":          {&v1.ListEventsRequest{Kinds: []string{"consumer.deleted"}}, 1},
		"stream":        {&v1.ListEventsRequest{Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "APP/order-jobs"}}, 4},
		"client":        {&v1.ListEventsRequest{ClientName: "analytics-worker"}, 1},
		"last 5 hours":  {&v1.ListEventsRequest{Range: &v1.TimeRange{Start: tsAt(9, 30, 0)}}, 11},
		"server nats-2": {&v1.ListEventsRequest{Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-2"}, Kinds: []string{"k8s.pod.killing", "k8s.pod.started"}}, 2},
	}
	for name, f := range filters {
		if got := must(c.ListEvents(ctx, connect.NewRequest(f.req))).GetTotalCount(); got != f.want {
			t.Errorf("%s: %d events, want %d", name, got, f.want)
		}
	}
	ev := must(c.GetEvent(ctx, connect.NewRequest(&v1.GetEventRequest{Id: del.GetId()}))).GetEvent()
	if !strings.Contains(ev.GetRaw(), "analytics-aggregator") {
		t.Fatalf("raw = %s", ev.GetRaw())
	}
	if _, err := c.GetEvent(ctx, connect.NewRequest(&v1.GetEventRequest{Id: "99999"})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown event: %v", err)
	}
}

func TestStreamsAndConsumers(t *testing.T) {
	c := client(t)
	st := must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{})))
	if len(st.GetStreams()) != 8 || st.GetTotals().GetDurableConsumers() != 13 || st.GetTotals().GetEphemeralConsumers() != 41 {
		t.Fatalf("%d streams, totals %v", len(st.GetStreams()), st.GetTotals())
	}
	top := st.GetStreams()[0]
	if top.GetEntity().GetName() != "order-archive" || top.GetStatus() != v1.Status_STATUS_CRITICAL ||
		top.GetConsumers() != 2 || top.GetConsumersExpected() != 3 || len(top.GetBytes_7D().GetPoints()) != 29 { // 28 whole 6-hour buckets and the partial one the range starts in
		t.Fatalf("worst stream = %v", top)
	}
	var muted *v1.StreamSummary
	for _, s := range st.GetStreams() {
		if s.GetEntity().GetName() == "audit-archive" {
			muted = s
		}
	}
	if muted.GetStatus() != v1.Status_STATUS_MUTED || muted.GetFindingCodes()[0] != "stream-031 muted" {
		t.Fatalf("audit-archive = %v", muted)
	}
	if n := len(must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{IncludeKv: true}))).GetStreams()); n != 9 {
		t.Fatalf("with kv: %d", n)
	}
	gs := must(c.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{Account: "APP", Stream: "order-archive"})))
	if len(gs.GetServers()) != 3 || len(gs.GetConsumers()) != 3 || gs.GetConsumers()[2].GetPresent() || len(gs.GetFindings()) != 2 {
		t.Fatalf("stream detail: %d servers, consumers %v, %d findings", len(gs.GetServers()), gs.GetConsumers(), len(gs.GetFindings()))
	}
	if _, err := c.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{Account: "APP", Stream: "nope"})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown stream: %v", err)
	}
	if _, err := c.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{Stream: "x"})); codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("missing account: %v", err)
	}
	cs := must(c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{})))
	if len(cs.GetConsumers()) != 14 || cs.GetConsumers()[0].GetEntity().GetName() != "analytics-aggregator" {
		t.Fatalf("consumers: %d, first %v", len(cs.GetConsumers()), cs.GetConsumers()[0].GetEntity())
	}
	if n := len(must(c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{IncludeEphemeral: true}))).GetConsumers()); n != 55 {
		t.Fatalf("with ephemeral: %d", n)
	}
	gc := must(c.GetConsumer(ctx, connect.NewRequest(&v1.GetConsumerRequest{Account: "APP", Stream: "order-jobs", Consumer: "resize-worker"})))
	if len(gc.GetDisagreements()) != 1 || gc.GetDisagreements()[0].GetField() != "created" || len(gc.GetServers()) != 3 || !gc.GetConsumer().GetServersDisagree() {
		t.Fatalf("resize-worker: %v", gc)
	}
}

func TestServersAndConnections(t *testing.T) {
	c := client(t)
	sv := must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{})))
	if len(sv.GetServers()) != 3 || len(sv.GetServers()[1].GetRestarts()) != 1 || len(sv.GetMissing()) != 0 {
		t.Fatalf("servers = %v", sv)
	}
	gs := must(c.GetServer(ctx, connect.NewRequest(&v1.GetServerRequest{Name: "nats-2"})))
	if len(gs.GetEndpoints()) == 0 || len(gs.GetProcesses()) != 2 {
		t.Fatalf("nats-2 = %v", gs)
	}
	if _, err := c.GetServer(ctx, connect.NewRequest(&v1.GetServerRequest{Name: "nats-9"})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown server: %v", err)
	}
	conns := must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{PageSize: 100})))
	if conns.GetTotalCount() != 182 || len(conns.GetConnections()) != 100 || conns.GetNextPageToken() == "" {
		t.Fatalf("connections: %d total, %d on page", conns.GetTotalCount(), len(conns.GetConnections()))
	}
	rest := must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{PageSize: 100, PageToken: conns.GetNextPageToken()})))
	if len(rest.GetConnections()) != 82 || rest.GetNextPageToken() != "" {
		t.Fatalf("second page: %d, token %q", len(rest.GetConnections()), rest.GetNextPageToken())
	}
	if n := must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{Server: "nats-1"}))).GetTotalCount(); n != 60 {
		t.Fatalf("nats-1 connections = %d", n)
	}
	if n := must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{NameContains: "video"}))).GetTotalCount(); n == 0 {
		t.Fatal("no video connections")
	}
}

func TestTimeSeries(t *testing.T) {
	c := client(t)
	s := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "cluster.connections", MaxPoints: 96}))).GetSeries()
	if len(s.GetPoints()) != 96 || s.GetPoints()[95].GetValue() != 182 || s.GetStep().AsDuration() != 15*time.Minute || s.GetMin() >= s.GetMax() {
		t.Fatalf("series: %d points, step %v, min %v max %v", len(s.GetPoints()), s.GetStep().AsDuration(), s.GetMin(), s.GetMax())
	}
	pend := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{
		Metric: "consumer.num_pending", MaxPoints: 2000,
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: "APP/order-archive/analytics-aggregator"},
		Range:  &v1.TimeRange{Start: tsAt(9, 0, 0), End: tsAt(10, 0, 0)},
	}))).GetSeries()
	if n := len(pend.GetPoints()); n != 63 {
		t.Fatalf("deleted consumer has %d points in 09:00-10:00, want 63 (data until 09:31:07)", n)
	}
	bad := map[string]*v1.GetTimeSeriesRequest{
		"unknown metric":  {Metric: "cluster.nope"},
		"missing entity":  {Metric: "stream.bytes"},
		"wrong type":      {Metric: "stream.bytes", Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-0"}},
		"start after end": {Metric: "cluster.connections", Range: &v1.TimeRange{Start: tsAt(12, 0, 0), End: tsAt(11, 0, 0)}},
	}
	for name, req := range bad {
		if _, err := c.GetTimeSeries(ctx, connect.NewRequest(req)); codeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: %v", name, err)
		}
	}
	_, err := c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "stream.bytes",
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "APP/nope"}}))
	if codeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown stream: %v", err)
	}
}

func TestHTTPRoutes(t *testing.T) {
	srv, _ := apitest.Serve(t, fake.New(false))
	get := func(path string) (int, string) {
		res, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if code, body := get("/healthz"); code != 200 || body != "ok\n" {
		t.Fatalf("healthz: %d %q", code, body)
	}
	for _, p := range []string{"/", "/streams/order-jobs"} {
		if code, body := get(p); code != 200 || !strings.Contains(body, "<title>nats-lens</title>") {
			t.Fatalf("%s: %d", p, code)
		}
	}
	if code, _ := get("/api/unknown"); code != http.StatusNotFound {
		t.Fatalf("/api/unknown: %d", code)
	}
}

func TestGRPCWebAndGRPC(t *testing.T) {
	web := client(t, connect.WithGRPCWeb())
	if o := must(web.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{}))); o.GetCluster() != fake.Cluster {
		t.Fatalf("grpc-web cluster = %q", o.GetCluster())
	}
	// gRPC needs HTTP/2; Serve offers it without TLS (h2c).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sctx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- api.ServeListener(sctx, ln, api.NewMux(&api.Handler{Systems: api.One(fake.Cluster, fake.Cluster, fake.New(false))}, webui.FS()), slog.New(slog.DiscardHandler))
	}()
	var protocols http.Protocols
	protocols.SetUnencryptedHTTP2(true)
	h2c := &http.Client{Transport: &http.Transport{Protocols: &protocols}}
	grpc := natslensv1connect.NewLensServiceClient(h2c, "http://"+ln.Addr().String()+api.PathPrefix, connect.WithGRPC())
	if o := must(grpc.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{}))); len(o.GetServers()) != 3 {
		t.Fatalf("grpc servers = %d", len(o.GetServers()))
	}
	stop()
	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

func TestLiveModeFollowsTheClock(t *testing.T) {
	p := fake.New(true)
	if d := time.Since(p.Now()); d < 0 || d > 2*time.Second {
		t.Fatalf("live now is %v from the clock", d)
	}
	_, c := apitest.Serve(t, p)
	o := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{})))
	since := o.GetWorstFinding().GetSince().AsTime()
	if age := o.GetRange().GetEnd().AsTime().Sub(since); age != 4*time.Hour+58*time.Minute+53*time.Second {
		t.Fatalf("worst finding age %v, want 4h58m53s", age)
	}
}

// The expected durable missing from every server reports no numbers.
func TestMissingConsumerReportsNoNumbers(t *testing.T) {
	c := client(t)
	cs := must(c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{Stream: "order-archive"}))).GetConsumers()
	for _, x := range cs {
		missing := !x.GetPresent()
		if missing != (x.NumAckPending == nil && x.NumRedelivered == nil && x.NumPending == nil && x.NumWaiting == nil) {
			t.Errorf("%s (present %v) = %v", x.GetEntity().GetName(), x.GetPresent(), x)
		}
	}
	gs := must(c.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{Account: "APP", Stream: "order-jobs"})))
	for _, v := range gs.GetServers() {
		if v.GetIsLeader() != (v.Lag == nil) {
			t.Errorf("%s: leader %v, lag %v", v.GetServer(), v.GetIsLeader(), v.Lag)
		}
	}
}

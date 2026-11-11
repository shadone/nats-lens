package storeprov_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/events"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

func emptyStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestFollowerOnlyConsumerHasNoBacklog(t *testing.T) {
	cs := must(client(t).ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{Range: hour()}))).GetConsumers()
	for _, c := range cs {
		switch c.GetEntity().GetName() {
		case "catalog-writer":
			if c.NumPending != nil || c.NumWaiting != nil || c.GetLeader() != "nats-2" {
				t.Errorf("no leader row, so no backlog: %v", c)
			}
		case "index-worker":
			if c.NumPending == nil || c.NumWaiting == nil {
				t.Errorf("leader row, backlog expected: %v", c)
			}
		}
		if c.GetStatus() != v1.Status_STATUS_NO_DATA {
			t.Errorf("%s status = %v before checks exist", c.GetEntity().GetName(), c.GetStatus())
		}
	}
}

func TestEmptyStore(t *testing.T) {
	_, c := serve(t, emptyStore(t))
	o := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{})))
	if o.GetFreshness().GetEpoch() != 0 || len(o.GetServers()) != 0 {
		t.Fatalf("overview = %v", o)
	}
	for _, tile := range o.GetTiles() {
		if tile.Value != nil || tile.Value2 != nil || tile.GetStatus() != v1.Status_STATUS_NO_DATA || tile.GetValueSuffix() == "/ 0" {
			t.Errorf("tile %s on an empty store: %v", tile.GetId(), tile)
		}
	}
	calls := map[string]func() error{
		"ListServers":   func() error { _, e := c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{})); return e },
		"ListStreams":   func() error { _, e := c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{})); return e },
		"ListConsumers": func() error { _, e := c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{})); return e },
		"ListConnections": func() error {
			_, e := c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{}))
			return e
		},
		"ListEvents":  func() error { _, e := c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{})); return e },
		"ListMutes":   func() error { _, e := c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{})); return e },
		"ListMetrics": func() error { _, e := c.ListMetrics(ctx, connect.NewRequest(&v1.ListMetricsRequest{})); return e },
		"ListSystems": func() error {
			res, e := c.ListSystems(ctx, connect.NewRequest(&v1.ListSystemsRequest{}))
			if e == nil && res.Msg.GetSystems()[0].GetError() != "" {
				e = errors.New(res.Msg.GetSystems()[0].GetError())
			}
			return e
		},
		"GetStatusTimeline": func() error {
			_, e := c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{}))
			return e
		},
		"GetTimeSeries": func() error {
			_, e := c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "cluster.in_msgs_rate"}))
			return e
		},
	}
	for name, f := range calls {
		if err := f(); err != nil {
			t.Errorf("%s on an empty store: %v", name, err)
		}
	}
}

func TestFreshnessIsBoundedByTheRange(t *testing.T) {
	c := client(t)
	after := &v1.TimeRange{Start: timestamppb.New(t0.Add(70 * time.Second))}
	if f := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: after}))).GetFreshness(); f.GetEpoch() != 0 {
		t.Fatalf("a range after the last epoch reported epoch %d", f.GetEpoch())
	}
	before := &v1.TimeRange{Start: timestamppb.New(t0.Add(-time.Hour)), End: timestamppb.New(t0.Add(45 * time.Second))}
	if f := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: before}))).GetFreshness(); f.GetEpoch() != 2 {
		t.Fatalf("a range ending before epoch 3 reported epoch %d", f.GetEpoch())
	}
}

func TestConnectionPagesKeepTheirEpoch(t *testing.T) {
	st := fixtureStore(t)
	_, c := serve(t, st)
	first := must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{Range: hour(), PageSize: 2})))
	if first.GetEpoch() != 3 || first.GetConnections()[0].GetKey() != "nats-0/7" {
		t.Fatalf("first page = %v", first)
	}
	// A new epoch arrives between the pages: epoch 4, inside the range.
	if _, err := st.WriteSnapshot(ctx, snapshot(t, 2)); err != nil {
		t.Fatal(err)
	}
	if fresh := must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{Range: hour()}))); fresh.GetEpoch() != 4 {
		t.Fatalf("a first page reads epoch %d, want 4", fresh.GetEpoch())
	}
	second := must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{Range: hour(), PageSize: 2,
		PageToken: first.GetNextPageToken()})))
	if second.GetEpoch() != 3 || len(second.GetConnections()) != 2 || second.GetConnections()[0].GetServer() != "nats-1" {
		t.Fatalf("second page = %v", second)
	}
	gone := api.EncodeCursor(api.Cursor{Offset: 2, Epoch: 99})
	_, err := c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{Range: hour(), PageToken: gone}))
	if codeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("token of a removed epoch: %v", err)
	}
}

func TestEventEntityFilters(t *testing.T) {
	c := client(t)
	count := func(e *v1.EntityRef) (int32, error) {
		res, err := c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: hour(), Entity: e}))
		if err != nil {
			return 0, err
		}
		return res.Msg.GetTotalCount(), nil
	}
	if n, err := count(&v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONNECTION, Key: "nats-1/7"}); err != nil || n != 1 {
		t.Fatalf("connection nats-1/7: %d, %v", n, err)
	}
	if n, err := count(&v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONNECTION, Key: "nats-0/7"}); err != nil || n != 0 {
		t.Fatalf("connection nats-0/7: %d, %v", n, err)
	}
	if n, err := count(&v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SYSTEM}); err != nil || n != 5 {
		t.Fatalf("system: %d, %v", n, err)
	}
	for _, e := range []*v1.EntityRef{
		{Type: v1.EntityType_ENTITY_TYPE_UNSPECIFIED, Key: "nope"},
		{Type: 99, Key: "k"},
		{Type: v1.EntityType_ENTITY_TYPE_CONNECTION, Key: "nats-0/abc"},
		{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "only-a-stream"},
		{Type: v1.EntityType_ENTITY_TYPE_SYSTEM, Key: "x"},
	} {
		if _, err := count(e); codeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("entity %v: %v", e, err)
		}
	}
}

func TestEventPagingWithEqualTimes(t *testing.T) {
	st := emptyStore(t)
	var evs []events.Event
	for range 7 {
		evs = append(evs, events.Event{Time: t0.Add(time.Second), ReceivedAt: t0.Add(2 * time.Second), Source: events.SourceSys,
			Kind: "client.connect", Summary: "s", Raw: json.RawMessage(`{}`)})
	}
	if err := st.WriteEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
	_, c := serve(t, st)
	seen := map[string]bool{}
	tok := ""
	for range 10 {
		r := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{PageSize: 2, PageToken: tok,
			Range: &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(now)}})))
		for _, e := range r.GetEvents() {
			if seen[e.GetId()] {
				t.Fatalf("event %s twice", e.GetId())
			}
			seen[e.GetId()] = true
		}
		if tok = r.GetNextPageToken(); tok == "" {
			break
		}
	}
	if len(seen) != 7 {
		t.Fatalf("paged %d of 7 events", len(seen))
	}
}

func TestRequestLimits(t *testing.T) {
	srv, c := serve(t, fixtureStore(t))
	invalidCalls := map[string]func() error{
		"series over 8000 years": func() error {
			_, e := c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "cluster.connections", MaxPoints: 1,
				Range: &v1.TimeRange{Start: timestamppb.New(time.Date(1, 1, 2, 0, 0, 0, 0, time.UTC)),
					End: timestamppb.New(time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC))}}))
			return e
		},
		"2001 buckets": func() error {
			_, e := c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{
				Range:  &v1.TimeRange{Start: timestamppb.New(now.Add(-(2000*time.Second + 500*time.Millisecond))), End: timestamppb.New(now)},
				Bucket: durationpb.New(time.Second)}))
			return e
		},
		"huge offset": func() error {
			tok := base64.RawURLEncoding.EncodeToString([]byte(`{"o":9223372036854775807}`))
			_, e := c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{Range: hour(), PageToken: tok}))
			return e
		},
		"51 kinds": func() error {
			_, e := c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Kinds: make([]string, 51)}))
			return e
		},
	}
	for name, f := range invalidCalls {
		if err := f(); codeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A body above the message limit is refused before it is decoded.
	body := `{"checkCode":"x-001","reason":"` + strings.Repeat("x", 2<<20) + `","mutedBy":"me","expiresAt":"2026-10-17T00:00:00Z"}`
	res, err := http.Post(srv.URL+"/api/natslens.v1.LensService/MuteFinding", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode == http.StatusOK {
		t.Fatal("a 2 MiB request was accepted")
	}
}

func TestMuteValidation(t *testing.T) {
	c := client(t)
	ok := func() *v1.MuteFindingRequest {
		return &v1.MuteFindingRequest{CheckCode: "stream-025", Reason: "r", MutedBy: "me", ExpiresAt: timestamppb.New(now.Add(time.Hour))}
	}
	bad := map[string]func(*v1.MuteFindingRequest){
		"expires in 1 ns":    func(m *v1.MuteFindingRequest) { m.ExpiresAt = timestamppb.New(now.Add(time.Nanosecond)) },
		"expires in 59 s":    func(m *v1.MuteFindingRequest) { m.ExpiresAt = timestamppb.New(now.Add(59 * time.Second)) },
		"expires in 91 days": func(m *v1.MuteFindingRequest) { m.ExpiresAt = timestamppb.New(now.Add(91 * 24 * time.Hour)) },
		"expires in year 9999": func(m *v1.MuteFindingRequest) {
			m.ExpiresAt = timestamppb.New(time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC))
		},
		"check code":            func(m *v1.MuteFindingRequest) { m.CheckCode = "x-1" },
		"reason of 1001 chars":  func(m *v1.MuteFindingRequest) { m.Reason = strings.Repeat("é", 1001) },
		"muted_by of 257 chars": func(m *v1.MuteFindingRequest) { m.MutedBy = strings.Repeat("a", 257) },
		"entity type 99":        func(m *v1.MuteFindingRequest) { m.Entity = &v1.EntityRef{Type: 99, Key: "k"} },
		"entity without key":    func(m *v1.MuteFindingRequest) { m.Entity = &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER} },
	}
	for name, change := range bad {
		req := ok()
		change(req)
		if _, err := c.MuteFinding(ctx, connect.NewRequest(req)); codeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: %v", name, err)
		}
	}
	req := ok()
	req.ExpiresAt = timestamppb.New(now.Add(time.Hour + 900*time.Millisecond))
	m := must(c.MuteFinding(ctx, connect.NewRequest(req))).GetMute()
	if !m.GetExpiresAt().AsTime().Equal(now.Add(time.Hour)) {
		t.Fatalf("expiry not truncated to seconds: %v", m.GetExpiresAt().AsTime())
	}
	// Without IAP configured, the unsigned IAP email header is ignored.
	spoof := connect.NewRequest(ok())
	spoof.Header().Set("X-Goog-Authenticated-User-Email", "accounts.google.com:ceo@example.com")
	if m := must(c.MuteFinding(ctx, spoof)).GetMute(); m.GetMutedBy() != "me" {
		t.Fatalf("muted_by = %q", m.GetMutedBy())
	}
	var ids []string
	tok := ""
	for {
		p := must(c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{PageSize: 1, PageToken: tok})))
		if p.GetTotalCount() != 2 {
			t.Fatalf("total = %d", p.GetTotalCount())
		}
		for _, m := range p.GetMutes() {
			ids = append(ids, m.GetId())
		}
		if tok = p.GetNextPageToken(); tok == "" {
			break
		}
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("paged mutes %v", ids)
	}
}

func TestSeriesBucketsAlignToTheUnixEpoch(t *testing.T) {
	c := client(t)
	// One bucket of an hour over a range that starts mid-hour: the bucket
	// starts at the hour, before the range.
	s := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "cluster.servers_up", MaxPoints: 1, Range: hour()}))).GetSeries()
	if len(s.GetPoints()) != 1 || !s.GetPoints()[0].GetTime().AsTime().Equal(t0.Truncate(time.Hour)) {
		t.Fatalf("points = %v, want one at %v", s.GetPoints(), t0.Truncate(time.Hour))
	}
	// The ListStreams sparkline is the stream.bytes series of the week.
	st := must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Range: hour()}))).GetStreams()[1]
	week := &v1.TimeRange{Start: timestamppb.New(now.Add(-7 * 24 * time.Hour)), End: timestamppb.New(now)}
	want := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "stream.bytes", MaxPoints: 28,
		Range: week, Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: st.GetEntity().GetKey()}}))).GetSeries()
	got := st.GetBytes_7D()
	if len(got.GetPoints()) == 0 || len(got.GetPoints()) != len(want.GetPoints()) || got.GetStep().AsDuration() != want.GetStep().AsDuration() {
		t.Fatalf("sparkline %v, series %v", got, want)
	}
	for i := range got.GetPoints() {
		if !got.GetPoints()[i].GetTime().AsTime().Equal(want.GetPoints()[i].GetTime().AsTime()) || got.GetPoints()[i].GetValue() != want.GetPoints()[i].GetValue() {
			t.Fatalf("point %d: %v vs %v", i, got.GetPoints()[i], want.GetPoints()[i])
		}
	}
}

// A row the mutes table rejects is a server fault: internal, never
// invalid_argument, and the database's text stays in the log.
func TestMuteConstraintFaultIsInternal(t *testing.T) {
	p := &storeprov.Provider{St: emptyStore(t), Interval: 30 * time.Second, Clock: func() time.Time { return now }}
	_, err := p.MuteFinding(ctx, &v1.MuteFindingRequest{CheckCode: "stream-025", Reason: "r", MutedBy: "op", ExpiresAt: timestamppb.New(now)})
	if err == nil || errors.Is(err, api.ErrInvalid) {
		t.Fatalf("constraint fault: %v", err)
	}
	_, c := apitest.Serve(t, &faultyMutes{p})
	_, err = c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{CheckCode: "stream-025", Reason: "r", MutedBy: "op",
		ExpiresAt: timestamppb.New(now.Add(time.Hour))}))
	if codeOf(err) != connect.CodeInternal || strings.Contains(err.Error(), "Constraint") {
		t.Fatalf("through the API: %v", err)
	}
}

// faultyMutes stores every mute with an expiry equal to its creation, which
// the table rejects.
type faultyMutes struct{ *storeprov.Provider }

func (f *faultyMutes) MuteFinding(c context.Context, req *v1.MuteFindingRequest) (*v1.Mute, error) {
	req.ExpiresAt = timestamppb.New(now)
	return f.Provider.MuteFinding(c, req)
}

// Mute pages continue below the last ID of the previous page, so a mute
// created between two pages does not repeat an entry.
func TestMutePagesKeepTheirPlace(t *testing.T) {
	c := client(t)
	mute := func(code string) string {
		return must(c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{CheckCode: code, Reason: "r", MutedBy: "op",
			ExpiresAt: timestamppb.New(now.Add(time.Hour))}))).GetMute().GetId()
	}
	older, newer := mute("stream-001"), mute("stream-002")
	first := must(c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{PageSize: 1})))
	if len(first.GetMutes()) != 1 || first.GetMutes()[0].GetId() != newer {
		t.Fatalf("first page = %v", first.GetMutes())
	}
	mute("stream-003")
	second := must(c.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{PageSize: 1, PageToken: first.GetNextPageToken()})))
	if len(second.GetMutes()) != 1 || second.GetMutes()[0].GetId() != older || second.GetNextPageToken() != "" {
		t.Fatalf("second page = %v, token %q", second.GetMutes(), second.GetNextPageToken())
	}
	for _, id := range []string{"007", "+7", "0", "-1", " 1"} {
		if _, err := c.UnmuteFinding(ctx, connect.NewRequest(&v1.UnmuteFindingRequest{MuteId: id, UnmutedBy: "op"})); codeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("mute_id %q: %v", id, err)
		}
		if _, err := c.GetEvent(ctx, connect.NewRequest(&v1.GetEventRequest{Id: id})); codeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("event id %q: %v", id, err)
		}
	}
}

func TestTimelineLaneTruncated(t *testing.T) {
	st := emptyStore(t)
	var evs []events.Event
	for i := range 1001 {
		evs = append(evs, events.Event{Time: t0.Add(time.Duration(i) * time.Second), ReceivedAt: t0.Add(time.Duration(i) * time.Second),
			Source: events.SourceJetStream, Kind: "stream.quorum_lost", Summary: "s", Raw: json.RawMessage(`{}`)})
	}
	if err := st.WriteEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
	_, c := serve(t, st)
	r := &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(time.Hour))}
	tl := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: r})))
	lane := tl.GetNats()
	if !tl.GetNatsTruncated() || len(lane) != 1000 || !lane[0].GetTime().AsTime().Equal(t0.Add(time.Second)) ||
		!lane[999].GetTime().AsTime().Equal(t0.Add(1000*time.Second)) {
		t.Fatalf("truncated %v, %d items from %v to %v", tl.GetNatsTruncated(), len(lane), lane[0].GetTime().AsTime(), lane[len(lane)-1].GetTime().AsTime())
	}
	r.Start = timestamppb.New(t0.Add(time.Second))
	if tl := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: r}))); tl.GetNatsTruncated() || len(tl.GetNats()) != 1000 {
		t.Fatalf("exactly 1000: truncated %v, %d", tl.GetNatsTruncated(), len(tl.GetNats()))
	}
}

// The Kubernetes lane is cut at the limit like the NATS lane, and says so.
func TestTimelineKubernetesLaneTruncated(t *testing.T) {
	st := emptyStore(t)
	var evs []events.Event
	for i := range 1001 {
		at := t0.Add(time.Duration(i) * time.Second)
		evs = append(evs, events.Event{Time: at, ReceivedAt: at, Source: events.SourceK8s, Kind: "k8s.pod.killing",
			ServerName: "nats-0", Summary: "s", Raw: json.RawMessage(`{"node":"n0"}`)})
	}
	if err := st.WriteEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
	_, c := serve(t, st)
	r := &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(time.Hour))}
	tl := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: r})))
	lane := tl.GetKubernetes()
	if !tl.GetKubernetesTruncated() || len(lane) != 1000 || !lane[0].GetTime().AsTime().Equal(t0.Add(time.Second)) ||
		!lane[999].GetTime().AsTime().Equal(t0.Add(1000*time.Second)) || tl.GetNatsTruncated() {
		t.Fatalf("truncated %v, %d items", tl.GetKubernetesTruncated(), len(lane))
	}
	r.Start = timestamppb.New(t0.Add(time.Second))
	if tl := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: r}))); tl.GetKubernetesTruncated() || len(tl.GetKubernetes()) != 1000 {
		t.Fatalf("exactly 1000: truncated %v, %d", tl.GetKubernetesTruncated(), len(tl.GetKubernetes()))
	}
}

// A number the servers did not report is unset, not zero.
func TestUnreportedNumbersAreUnset(t *testing.T) {
	st := fixtureStore(t)
	for _, q := range []string{
		`UPDATE server_stats SET connections = NULL, js_storage = NULL, mem_bytes = NULL, cpu_percent = NULL, subscriptions = NULL, routes = NULL WHERE epoch = 3 AND server_id = 'NFAKESERVER0ID'`,
		`UPDATE connections SET in_msgs = NULL, port = NULL, stalls = NULL WHERE epoch = 3 AND cid = 9`,
		`UPDATE stream_stats SET messages = NULL, bytes = NULL, first_seq = NULL WHERE epoch = 3 AND stream = 'catalog-changes'`,
		`UPDATE stream_config SET replicas = NULL WHERE stream = 'catalog-changes'`,
		`UPDATE consumer_stats SET num_ack_pending = NULL, num_redelivered = NULL, delivered_stream_seq = NULL WHERE epoch = 3 AND consumer = 'index-worker'`,
	} {
		if _, err := st.DB().Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	_, c := serve(t, st)
	n0 := must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{Range: hour()}))).GetServers()[0]
	if n0.Connections != nil || n0.JsStorageBytes != nil || n0.MemBytes != nil || n0.CpuPercent != nil || n0.JsMaxStorageBytes == nil {
		t.Errorf("nats-0 = %v", n0)
	}
	gs := must(c.GetServer(ctx, connect.NewRequest(&v1.GetServerRequest{Name: "nats-0", Range: hour()})))
	if gs.Subscriptions != nil || gs.Routes != nil || gs.SlowConsumers == nil {
		t.Errorf("nats-0 detail = %v", gs)
	}
	for _, cn := range must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{Range: hour()}))).GetConnections() {
		if (cn.GetCid() == 9) != (cn.InMsgs == nil && cn.Port == nil && cn.Stalls == nil) || cn.OutMsgs == nil {
			t.Errorf("connection %s = %v", cn.GetKey(), cn)
		}
	}
	gstr := must(c.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{Account: account, Stream: "catalog-changes", Range: hour()})))
	s := gstr.GetStream()
	if s.Messages != nil || s.Bytes != nil || s.Replicas != nil || s.BytesPerDay != nil {
		t.Errorf("catalog-changes = %v", s)
	}
	for _, v := range gstr.GetServers() {
		if v.Messages != nil || v.FirstSeq != nil || v.LastSeq == nil || (v.GetIsLeader() && v.Lag != nil) {
			t.Errorf("catalog-changes on %s = %v", v.GetServer(), v)
		}
	}
	gc := must(c.GetConsumer(ctx, connect.NewRequest(&v1.GetConsumerRequest{Account: account, Stream: "order-jobs", Consumer: "index-worker", Range: hour()})))
	if gc.GetConsumer().NumAckPending != nil || gc.GetConsumer().NumRedelivered != nil || gc.GetConsumer().Replicas == nil {
		t.Errorf("index-worker = %v", gc.GetConsumer())
	}
	for _, v := range gc.GetServers() {
		if v.DeliveredStreamSeq != nil || v.AckFloorStreamSeq == nil {
			t.Errorf("index-worker on %s = %v", v.GetServer(), v)
		}
	}
	ok := must(c.GetConsumer(ctx, connect.NewRequest(&v1.GetConsumerRequest{Account: account, Stream: "order-jobs", Consumer: "resize-worker", Range: hour()})))
	if ok.GetConsumer().NumAckPending == nil || ok.GetConsumer().NumRedelivered == nil {
		t.Errorf("resize-worker = %v", ok.GetConsumer())
	}
}

func TestLensInfoFromTheStore(t *testing.T) {
	p := &storeprov.Provider{St: fixtureStore(t), Interval: 30 * time.Second, Retention: 14 * 24 * time.Hour, Live: true, Clock: func() time.Time { return now }}
	_, c := apitest.Serve(t, p)
	m := must(c.GetLensInfo(ctx, connect.NewRequest(&v1.GetLensInfoRequest{})))
	if m.GetCluster() != "example-dev" || m.GetRetention().AsDuration() != 14*24*time.Hour || m.GetDemo() || !m.GetLive() ||
		m.GetIdentityMode() != v1.IdentityMode_IDENTITY_MODE_REQUEST {
		t.Fatalf("info = %v", m)
	}
	_, empty := serve(t, emptyStore(t))
	if m := must(empty.GetLensInfo(ctx, connect.NewRequest(&v1.GetLensInfoRequest{}))); m.GetCluster() != "" {
		t.Fatalf("empty store cluster = %q", m.GetCluster())
	}
}

// A tile's current value is its chart's last point, so the two use the
// same reduction: with 9 points over an hour (400-second buckets) the last
// bucket holds all three epochs, whose in rates are none, 20 and 10
// messages per second, and a rate's bucket is their average.
func TestTileValueIsTheChartsLastPoint(t *testing.T) {
	o := must(client(t).GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: hour(), MaxPoints: 9})))
	for _, tile := range o.GetTiles() {
		if tile.GetId() == "msgs_rate" && tile.GetValue() != 15 {
			t.Fatalf("in rate tile = %v, want 15 (the last bucket's average)", tile.GetValue())
		}
	}
}

// The band query stays far within the call's time limit over a week of
// 30-second epochs from 24 servers (20 000 epochs, 480 000 server rows).
func TestBandOverALargeStore(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 480 000 server rows")
	}
	st := emptyStore(t)
	const epochs, servers = 20000, 24
	begin := now.Add(-epochs * 30 * time.Second)
	// As text: a bound time.Time is cast through the local time zone.
	at := begin.Format(time.DateTime)
	for _, q := range []string{
		`INSERT INTO epochs SELECT i, ?::TIMESTAMP + to_seconds(i * 30), ?::TIMESTAMP + to_seconds(i * 30), 0, 24, 24, [], [], [], 0, false, [], NULL
		 FROM range(?) t(i)`,
		`INSERT INTO servers (server_id, server_name, first_epoch, last_epoch, first_seen_at, last_seen_at)
		 SELECT 'S' || s, 'nats-' || s, 0, ? - 1, ?::TIMESTAMP, ?::TIMESTAMP FROM range(24) t(s)`,
		`INSERT INTO server_stats (epoch, server_id, cpu_percent, mem_bytes)
		 SELECT e, 'S' || s, s * 3 + (hash(e, s) % 100) / 50.0, (100 + s) * 1048576 FROM range(?) a(e), range(24) b(s)`,
	} {
		var args []any
		switch {
		case strings.HasPrefix(strings.TrimSpace(q), "INSERT INTO epochs"):
			args = []any{at, at, epochs}
		case strings.Contains(q, "INTO servers"):
			args = []any{epochs, at, at}
		default:
			args = []any{epochs}
		}
		if _, err := st.DB().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	_, c := serve(t, st)
	for _, points := range []int32{200, 2000} {
		started := time.Now()
		s := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "cluster.cpu_percent", MaxPoints: points,
			Range: &v1.TimeRange{Start: timestamppb.New(begin), End: timestamppb.New(now)}}))).GetSeries()
		took := time.Since(started)
		t.Logf("band over %d epochs x %d servers, %d points: %v", epochs, servers, points, took)
		if s.GetTop().GetKey() != "nats-23" || len(s.GetPoints()) < int(points) || took > api.QueryTimeout/3 {
			t.Fatalf("band took %v with top %v and %d points", took, s.GetTop(), len(s.GetPoints()))
		}
	}
}

// 500 000 events over two weeks, with 40 000 epochs, read with routine
// hiding, folding, facets and a histogram: well within the call timeout.
func TestFoldedEventsAtScale(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 500 000 events")
	}
	st := emptyStore(t)
	start := now.Add(-14 * 24 * time.Hour)
	if _, err := st.DB().ExecContext(ctx, `
INSERT INTO epochs (epoch, started_at, finished_at, duration_ms, expected_servers, observed_servers, expected_names,
  observed_names, missing_names, endpoint_failures, partial, errors, loopback_tooling)
SELECT i + 1, ?::TIMESTAMP + to_seconds(i * 30), ?::TIMESTAMP + to_seconds(i * 30 + 1), 1000, 3, 3, [], [], [], 0, false, [],
  i % 7 <> 0
FROM range(40000) t(i)`, start, start); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().ExecContext(ctx, `
INSERT INTO events (id, time, received_at, source, kind, server_name, account, stream, consumer, client_name, client_host, reason, summary, raw)
SELECT nextval('event_seq'), ?::TIMESTAMP + to_milliseconds(i * 2419), ?::TIMESTAMP + to_milliseconds(i * 2419),
  ['sys', 'sys', 'jetstream', 'k8s', 'sys'][i % 5 + 1],
  ['client.churn', 'client.connect', 'consumer.ack_sampled', 'k8s.pod.failed_scheduling', 'client.disconnect'][i % 5 + 1],
  'nats-' || (i % 24), 'APP', 's' || (i % 40), NULL, 'app-' || (i % 50),
  CASE WHEN i % 11 = 0 THEN '127.0.0.1' ELSE '10.0.0.' || (i % 200) END, NULL, 'event ' || i,
  CASE WHEN i % 5 = 3 THEN json_object('uid', 'u' || i, 'count', i % 4 + 1) ELSE '{}'::JSON END
FROM range(500000) t(i)`, start, start); err != nil {
		t.Fatal(err)
	}
	_, c := serve(t, st)
	r := &v1.TimeRange{Start: timestamppb.New(start), End: timestamppb.New(now)}
	began := time.Now()
	res := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: r, HideRoutine: true, FoldRepeats: true,
		IncludeFacets: true, HistogramBuckets: 120, PageSize: 100})))
	first := time.Since(began)
	began = time.Now()
	next := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: r, HideRoutine: true, FoldRepeats: true,
		PageSize: 100, PageToken: res.GetNextPageToken()})))
	t.Logf("first page with facets and histogram %v, next page %v; %d events shown, %d hidden", first, time.Since(began),
		res.GetTotalCount(), res.GetRoutineHidden())
	if res.GetTotalUnfiltered() != 500000 || res.GetTotalCount()+res.GetRoutineHidden() != 500000 || len(res.GetEvents()) != 100 ||
		len(res.GetHistogram()) != 120 || len(next.GetEvents()) != 100 {
		t.Fatalf("unfiltered %d, total %d, hidden %d, %d rows, %d buckets", res.GetTotalUnfiltered(), res.GetTotalCount(),
			res.GetRoutineHidden(), len(res.GetEvents()), len(res.GetHistogram()))
	}
	if first > 10*time.Second {
		t.Fatalf("first page took %v", first)
	}
}

package api_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/webui"
)

// renamed is the fake scenario under another cluster name; it counts the
// calls that reach it.
type renamed struct {
	*fake.Provider
	cluster string
	calls   *atomic.Int32
	// metrics is "fail" to fail WriteMetrics after a partial write, "hang"
	// to block it until its context ends.
	metrics string
	// release ends a WriteMetrics in mode "ignore", which waits for it and
	// ignores its context.
	release chan struct{}
}

func newRenamed(cluster string) renamed {
	return renamed{Provider: fake.New(false), cluster: cluster, calls: &atomic.Int32{}}
}

func (r renamed) GetOverview(ctx context.Context, req *v1.GetOverviewRequest) (*v1.GetOverviewResponse, error) {
	r.calls.Add(1)
	res, err := r.Provider.GetOverview(ctx, req)
	if res != nil {
		res.Cluster = r.cluster
	}
	return res, err
}

func (r renamed) Info(ctx context.Context) (*v1.GetLensInfoResponse, error) {
	r.calls.Add(1)
	res, err := r.Provider.Info(ctx)
	if res != nil {
		res.Cluster = r.cluster
	}
	return res, err
}

// WriteMetrics writes two families, the second with a label.
func (r renamed) WriteMetrics(ctx context.Context, m *api.Exposition) error {
	r.calls.Add(1)
	m.Family("nats_lens_test_up", "gauge", "Test family without labels.")
	m.Sample("nats_lens_test_up", 1)
	m.Family("nats_lens_test_by_code", "gauge", "Test family with a label.")
	m.Sample("nats_lens_test_by_code", 2, "code", r.cluster+"-001")
	switch r.metrics {
	case "fail":
		return errors.New("metrics broke")
	case "hang":
		<-ctx.Done()
		return ctx.Err()
	case "ignore":
		<-r.release
		return nil
	case "panic":
		panic("metrics exploded")
	}
	return nil
}

func twoSystems() (api.Systems, renamed, renamed) {
	a, b := newRenamed("cluster-a"), newRenamed("cluster-b")
	return api.Systems{
		{Ref: &v1.SystemRef{Id: "a", Name: "System A", Environment: "production"}, Provider: a},
		{Ref: &v1.SystemRef{Id: "b", Name: "System B"}, Provider: b},
	}, a, b
}

func TestRoutingBySystem(t *testing.T) {
	systems, a, b := twoSystems()
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: systems})
	ctx := context.Background()
	for _, tc := range []struct{ system, cluster string }{{"", "cluster-a"}, {"a", "cluster-a"}, {"b", "cluster-b"}} {
		res, err := c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{System: tc.system}))
		if err != nil || res.Msg.GetCluster() != tc.cluster {
			t.Errorf("system %q: cluster %q, %v; want %q", tc.system, res.Msg.GetCluster(), err, tc.cluster)
		}
	}
	if a.calls.Load() != 2 || b.calls.Load() != 1 {
		t.Errorf("calls a=%d b=%d, want 2 and 1", a.calls.Load(), b.calls.Load())
	}
}

func TestUnknownSystemIsInvalid(t *testing.T) {
	systems, a, b := twoSystems()
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: systems})
	_, err := c.GetOverview(context.Background(), connect.NewRequest(&v1.GetOverviewRequest{System: "nope"}))
	if codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("unknown system: %v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, `"nope"`) || !strings.Contains(msg, "a, b") {
		t.Errorf("the error does not name the system and the known ones: %s", msg)
	}
	if n := a.calls.Load() + b.calls.Load(); n != 0 {
		t.Errorf("%d provider calls for an unknown system", n)
	}
	// Every RPC that takes a system refuses it, the new ones too.
	for name, call := range map[string]func() error{
		"ListAccounts": func() error {
			_, err := c.ListAccounts(context.Background(), connect.NewRequest(&v1.ListAccountsRequest{System: "nope"}))
			return err
		},
		"ListKeyValueStores": func() error {
			_, err := c.ListKeyValueStores(context.Background(), connect.NewRequest(&v1.ListKeyValueStoresRequest{System: "nope"}))
			return err
		},
		"GetLensInfo": func() error {
			_, err := c.GetLensInfo(context.Background(), connect.NewRequest(&v1.GetLensInfoRequest{System: "nope"}))
			return err
		},
		"MuteFinding": func() error {
			req := muteReq("")
			req.Msg.System = "nope"
			_, err := c.MuteFinding(context.Background(), req)
			return err
		},
	} {
		if err := call(); codeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s with an unknown system: %v", name, err)
		}
	}
}

func TestLensInfoNamesTheSystem(t *testing.T) {
	systems, _, _ := twoSystems()
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: systems})
	res, err := c.GetLensInfo(context.Background(), connect.NewRequest(&v1.GetLensInfoRequest{System: "b"}))
	if err != nil || res.Msg.GetSystem() != "b" || res.Msg.GetSystemName() != "System B" || res.Msg.GetCluster() != "cluster-b" {
		t.Fatalf("info = %v, %v", res, err)
	}
}

func TestListSystems(t *testing.T) {
	systems, _, _ := twoSystems()
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: systems})
	res, err := c.ListSystems(context.Background(), connect.NewRequest(&v1.ListSystemsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	m := res.Msg
	if m.GetDefaultSystem() != "a" || len(m.GetSystems()) != 2 || m.GetRange().GetStart() == nil || m.GetRange().GetEnd() == nil {
		t.Fatalf("ListSystems = %v", m)
	}
	if r := m.GetSystems()[0].GetSystem(); r.GetId() != "a" || r.GetName() != "System A" || r.GetEnvironment() != "production" {
		t.Errorf("first system = %v", r)
	}
	if r := m.GetSystems()[1].GetSystem(); r.GetId() != "b" || r.GetName() != "System B" {
		t.Errorf("second system = %v", r)
	}
}

func TestListAccountsFake(t *testing.T) {
	_, c := apitest.Serve(t, fake.New(false))
	res, err := c.ListAccounts(context.Background(), connect.NewRequest(&v1.ListAccountsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	accs := res.Msg.GetAccounts()
	if len(accs) != 1 {
		t.Fatalf("accounts = %v", accs)
	}
	if a := accs[0]; a.GetAccount() != fake.Account || a.GetIsSystem() || a.Connections == nil || a.GetConnections() != 182 ||
		a.GetStreams() != 13 || a.GetConsumers() != 54 {
		t.Errorf("account = %v", a)
	}
}

func TestBucketListsReachTheProvider(t *testing.T) {
	_, c := apitest.Serve(t, fake.New(false))
	kv, err := c.ListKeyValueStores(context.Background(), connect.NewRequest(&v1.ListKeyValueStoresRequest{}))
	if err != nil || len(kv.Msg.GetKeyValueStores()) != 3 {
		t.Errorf("key-value stores = %v, %v", kv, err)
	}
	obj, err := c.ListObjectStores(context.Background(), connect.NewRequest(&v1.ListObjectStoresRequest{}))
	if err != nil || len(obj.Msg.GetObjectStores()) != 2 {
		t.Errorf("object stores = %v, %v", obj, err)
	}
}

func TestMetricsBySystem(t *testing.T) {
	systems, _, _ := twoSystems()
	srv, _ := apitest.ServeHandler(t, &api.Handler{Systems: systems})
	res, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	body := string(b)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/metrics: %d\n%s", res.StatusCode, body)
	}
	want := `# HELP nats_lens_system_scrape_success 1 when the system's metrics were read for this scrape, 0 when they failed.
# TYPE nats_lens_system_scrape_success gauge
nats_lens_system_scrape_success{system="a"} 1
nats_lens_system_scrape_success{system="b"} 1
# HELP nats_lens_test_up Test family without labels.
# TYPE nats_lens_test_up gauge
nats_lens_test_up{system="a"} 1
nats_lens_test_up{system="b"} 1
# HELP nats_lens_test_by_code Test family with a label.
# TYPE nats_lens_test_by_code gauge
nats_lens_test_by_code{system="a",code="cluster-a-001"} 2
nats_lens_test_by_code{system="b",code="cluster-b-001"} 2
`
	if body != want {
		t.Errorf("/metrics =\n%s\nwant\n%s", body, want)
	}
}

func scrapeMetrics(t *testing.T, url string) (int, string) {
	t.Helper()
	res, err := http.Get(url + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

// A failing system leaves none of its samples, and the others are served.
func TestMetricsWithAFailingSystem(t *testing.T) {
	systems, _, b := twoSystems()
	b.metrics = "fail"
	systems[1].Provider = b
	srv, _ := apitest.ServeHandler(t, &api.Handler{Systems: systems})
	code, body := scrapeMetrics(t, srv.URL)
	if code != http.StatusOK {
		t.Fatalf("/metrics: %d\n%s", code, body)
	}
	for _, want := range []string{
		`nats_lens_system_scrape_success{system="a"} 1`,
		`nats_lens_system_scrape_success{system="b"} 0`,
		`nats_lens_test_up{system="a"} 1`,
		`nats_lens_test_by_code{system="a",code="cluster-a-001"} 2`,
	} {
		if !strings.Contains(body, want+"\n") {
			t.Errorf("missing %q in\n%s", want, body)
		}
	}
	if strings.Contains(body, `nats_lens_test_up{system="b"}`) || strings.Contains(body, "cluster-b-001") {
		t.Errorf("partial samples of the failed system:\n%s", body)
	}
}

// Only when every system fails is the scrape a 500.
func TestMetricsWhenEverySystemFails(t *testing.T) {
	systems, a, b := twoSystems()
	a.metrics, b.metrics = "fail", "fail"
	systems[0].Provider, systems[1].Provider = a, b
	srv, _ := apitest.ServeHandler(t, &api.Handler{Systems: systems})
	if code, body := scrapeMetrics(t, srv.URL); code != http.StatusInternalServerError {
		t.Fatalf("/metrics: %d\n%s", code, body)
	}
}

// A hung system uses up its own timeout, not the others'.
func TestMetricsWithAHungSystem(t *testing.T) {
	systems, a, _ := twoSystems()
	a.metrics = "hang"
	systems[0].Provider = a
	srv, _ := apitest.ServeHandler(t, &api.Handler{Systems: systems, MetricsTimeout: 200 * time.Millisecond})
	start := time.Now()
	code, body := scrapeMetrics(t, srv.URL)
	if code != http.StatusOK || !strings.Contains(body, `nats_lens_test_up{system="b"} 1`+"\n") ||
		!strings.Contains(body, `nats_lens_system_scrape_success{system="a"} 0`+"\n") {
		t.Fatalf("/metrics: %d\n%s", code, body)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("the scrape took %v", d)
	}
}

func TestEmptySystemIDIsRefused(t *testing.T) {
	if _, err := api.NewExposition(""); err == nil {
		t.Error("NewExposition(\"\") gave no error")
	}
	if err := (api.Systems{{Ref: &v1.SystemRef{Id: ""}, Provider: fake.New(false)}}).Validate(); err == nil {
		t.Error("a system without an ID passed Validate")
	}
	systems, _, _ := twoSystems()
	if err := systems.Validate(); err != nil {
		t.Errorf("two systems: %v", err)
	}
	dup := append(api.Systems{}, systems[0], systems[0])
	if err := dup.Validate(); err == nil {
		t.Error("a duplicate ID passed Validate")
	}
}

// A registry entry without an ID that reaches the handler anyway writes
// no unlabelled samples.
func TestMetricsNeverWithoutSystemLabel(t *testing.T) {
	systems, _, _ := twoSystems()
	systems[1].Ref = &v1.SystemRef{}
	srv := httptest.NewServer(api.NewMux(&api.Handler{Systems: systems, Log: slog.New(slog.DiscardHandler)}, webui.FS()))
	defer srv.Close()
	_, body := scrapeMetrics(t, srv.URL)
	for line := range strings.SplitSeq(body, "\n") {
		if line != "" && !strings.HasPrefix(line, "#") && !strings.Contains(line, `{system="`) {
			t.Errorf("sample without a system label: %q", line)
		}
	}
}

func TestHelpIsEscaped(t *testing.T) {
	m, err := api.NewExposition("a")
	if err != nil {
		t.Fatal(err)
	}
	m.Family("x", "gauge", "back\\slash and\nnewline")
	m.Sample("x", 1)
	var sb strings.Builder
	if _, err := m.WriteTo(&sb); err != nil {
		t.Fatal(err)
	}
	if want := "# HELP x back\\\\slash and\\nnewline\n"; !strings.Contains(sb.String(), want) {
		t.Errorf("HELP not escaped:\n%s", sb.String())
	}
}

func TestDefaultMetricsTimeout(t *testing.T) {
	if api.DefaultMetricsTimeout != 8*time.Second {
		t.Errorf("DefaultMetricsTimeout = %v, want 8s, below a 10s scrape_timeout", api.DefaultMetricsTimeout)
	}
}

// A provider that ignores its context has one WriteMetrics running at a
// time; scrapes meanwhile report the system failed at once.
func TestMetricsOneScrapePerSystem(t *testing.T) {
	systems, a, _ := twoSystems()
	a.metrics, a.release = "ignore", make(chan struct{})
	systems[0].Provider = a
	srv, _ := apitest.ServeHandler(t, &api.Handler{Systems: systems, MetricsTimeout: 50 * time.Millisecond})
	for i := range 5 {
		start := time.Now()
		code, body := scrapeMetrics(t, srv.URL)
		if code != http.StatusOK || !strings.Contains(body, `nats_lens_system_scrape_success{system="a"} 0`+"\n") {
			t.Fatalf("scrape %d: %d\n%s", i, code, body)
		}
		if i > 0 && time.Since(start) > 40*time.Millisecond {
			t.Errorf("scrape %d waited %v for a system already running", i, time.Since(start))
		}
	}
	if n := a.calls.Load(); n != 1 {
		t.Errorf("WriteMetrics calls = %d, want 1", n)
	}
	close(a.release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, body := scrapeMetrics(t, srv.URL)
		if strings.Contains(body, `nats_lens_system_scrape_success{system="a"} 1`+"\n") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the system never recovered:\n%s", body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A panic in one system's metrics fails that system, not the process.
func TestMetricsPanicFailsTheSystem(t *testing.T) {
	systems, a, _ := twoSystems()
	a.metrics = "panic"
	systems[0].Provider = a
	srv, _ := apitest.ServeHandler(t, &api.Handler{Systems: systems})
	code, body := scrapeMetrics(t, srv.URL)
	if code != http.StatusOK || !strings.Contains(body, `nats_lens_system_scrape_success{system="a"} 0`+"\n") ||
		!strings.Contains(body, `nats_lens_test_up{system="b"} 1`+"\n") {
		t.Fatalf("/metrics: %d\n%s", code, body)
	}
}

// summaryOf wraps a provider with a SystemSummary that blocks until its
// context ends ("hang"), blocks without looking at it ("ignore"), panics
// ("panic") or fails ("fail").
type summaryOf struct {
	renamed
	mode string
}

func (s summaryOf) SystemSummary(ctx context.Context, req *v1.ListSystemsRequest) (*v1.SystemSummary, error) {
	switch s.mode {
	case "hang":
		<-ctx.Done()
		return nil, ctx.Err()
	case "ignore":
		<-s.release
		return &v1.SystemSummary{}, nil
	case "panic":
		panic("summary exploded")
	case "fail":
		return nil, errors.New("database is gone")
	}
	return s.renamed.SystemSummary(ctx, req)
}

func listSystems(t *testing.T, h *api.Handler, req *v1.ListSystemsRequest) (*v1.ListSystemsResponse, time.Duration) {
	t.Helper()
	_, c := apitest.ServeHandler(t, h)
	start := time.Now()
	res, err := c.ListSystems(context.Background(), connect.NewRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg, time.Since(start)
}

// A system whose summary blocks past the timeout gets an error on its own
// card; the others still return, and the systems are read in parallel.
func TestListSystemsTimeoutStaysOnItsCard(t *testing.T) {
	for _, mode := range []string{"hang", "ignore"} {
		t.Run(mode, func(t *testing.T) {
			systems, a, b := twoSystems()
			a.release = make(chan struct{})
			defer close(a.release)
			third := newRenamed("cluster-c")
			third.release = a.release
			systems[0].Provider = summaryOf{a, mode}
			systems = append(systems, api.System{Ref: &v1.SystemRef{Id: "c"}, Provider: summaryOf{third, mode}})
			_ = b
			m, took := listSystems(t, &api.Handler{Systems: systems, SummaryTimeout: 200 * time.Millisecond}, &v1.ListSystemsRequest{})
			if len(m.GetSystems()) != 3 {
				t.Fatalf("systems = %v", m.GetSystems())
			}
			for i, want := range []bool{true, false, true} {
				s := m.GetSystems()[i]
				if got := s.GetError() != ""; got != want {
					t.Errorf("system %s: error %q", s.GetSystem().GetId(), s.GetError())
				}
			}
			if s := m.GetSystems()[1]; s.GetTally() == nil || s.GetServers() != 3 {
				t.Errorf("the healthy system lost its summary: %v", s)
			}
			if took > 380*time.Millisecond {
				t.Errorf("ListSystems took %v; two blocked systems should time out together", took)
			}
		})
	}
}

func TestListSystemsPanicAndFailureStayOnTheirCards(t *testing.T) {
	systems, a, b := twoSystems()
	systems[0].Provider = summaryOf{a, "panic"}
	systems = append(systems, api.System{Ref: &v1.SystemRef{Id: "c"}, Provider: summaryOf{newRenamed("c"), "fail"}})
	_ = b
	m, _ := listSystems(t, &api.Handler{Systems: systems, Log: slog.New(slog.DiscardHandler)}, &v1.ListSystemsRequest{})
	got := m.GetSystems()
	if got[0].GetError() == "" || got[1].GetError() != "" || got[2].GetError() == "" {
		t.Fatalf("errors = %q, %q, %q", got[0].GetError(), got[1].GetError(), got[2].GetError())
	}
	if strings.Contains(got[2].GetError(), "database is gone") || strings.Contains(got[0].GetError(), "exploded") {
		t.Errorf("a provider's error text reached the client: %q, %q", got[0].GetError(), got[2].GetError())
	}
	if got[0].GetSystem().GetId() != "a" || got[2].GetSystem().GetId() != "c" {
		t.Errorf("a failed card lost its reference: %v, %v", got[0].GetSystem(), got[2].GetSystem())
	}
}

// The runtime's collection status replaces what the provider read from
// stored events.
func TestListSystemsUsesTheCollectionStatus(t *testing.T) {
	systems, _, _ := twoSystems()
	since := time.Date(2026, 10, 17, 11, 30, 0, 0, time.UTC)
	systems[1].Status = func() api.CollectionStatus {
		return api.CollectionStatus{State: api.CollectionConnectionClosed, LastError: "nats: authorization violation", Since: since}
	}
	m, _ := listSystems(t, &api.Handler{Systems: systems}, &v1.ListSystemsRequest{})
	a, b := m.GetSystems()[0], m.GetSystems()[1]
	if a.GetCollection() != v1.CollectionState_COLLECTION_STATE_DEMO {
		t.Errorf("a without a status: %v", a.GetCollection())
	}
	if b.GetCollection() != v1.CollectionState_COLLECTION_STATE_CONNECTION_CLOSED ||
		b.GetCollectionError() != "nats: authorization violation" || !b.GetCollectionErrorSince().AsTime().Equal(since) {
		t.Errorf("b = %v %q %v", b.GetCollection(), b.GetCollectionError(), b.GetCollectionErrorSince())
	}
	systems[1].Status = func() api.CollectionStatus { return api.CollectionStatus{State: api.CollectionCollecting} }
	m, _ = listSystems(t, &api.Handler{Systems: systems}, &v1.ListSystemsRequest{})
	if b := m.GetSystems()[1]; b.GetCollection() != v1.CollectionState_COLLECTION_STATE_COLLECTING || b.GetCollectionError() != "" || b.GetCollectionErrorSince() != nil {
		t.Errorf("b collecting = %v %q %v", b.GetCollection(), b.GetCollectionError(), b.GetCollectionErrorSince())
	}
}

func TestListSystemsStripBuckets(t *testing.T) {
	systems, _, _ := twoSystems()
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: systems})
	for _, n := range []int32{-1, 501} {
		_, err := c.ListSystems(context.Background(), connect.NewRequest(&v1.ListSystemsRequest{StripBuckets: n}))
		if codeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("strip_buckets %d: %v", n, err)
		}
	}
	m, _ := listSystems(t, &api.Handler{Systems: systems}, &v1.ListSystemsRequest{})
	if n := len(m.GetSystems()[0].GetStrip()); n != 48 {
		t.Errorf("default strip has %d buckets", n)
	}
	m, _ = listSystems(t, &api.Handler{Systems: systems}, &v1.ListSystemsRequest{StripBuckets: 24})
	if s := m.GetSystems()[0]; len(s.GetStrip()) != 24 || s.GetStripBucket().AsDuration() != time.Hour {
		t.Errorf("24 buckets: %d of %v", len(s.GetStrip()), s.GetStripBucket())
	}
}

// A provider that ignores its context holds at most one goroutine per
// system: while its previous summary still runs, a call gets the timeout
// card at once.
func TestListSystemsStartsOneSummaryPerSystem(t *testing.T) {
	systems, a, _ := twoSystems()
	a.release = make(chan struct{})
	defer close(a.release)
	systems[0].Provider = summaryOf{a, "ignore"}
	h := &api.Handler{Systems: systems, SummaryTimeout: 20 * time.Millisecond}
	before := runtime.NumGoroutine()
	for i := range 30 {
		res, err := h.ListSystems(context.Background(), connect.NewRequest(&v1.ListSystemsRequest{}))
		if err != nil {
			t.Fatal(err)
		}
		if s := res.Msg.GetSystems()[0]; s.GetError() == "" {
			t.Fatalf("call %d: no error on the blocked system", i)
		}
	}
	time.Sleep(50 * time.Millisecond)
	if n := runtime.NumGoroutine() - before; n > 3 {
		t.Errorf("%d goroutines more after 30 calls", n)
	}
}

// A collection status that panics fails its own card only.
func TestListSystemsStatusPanicStaysOnItsCard(t *testing.T) {
	systems, _, _ := twoSystems()
	systems[1].Status = func() api.CollectionStatus { panic("status exploded") }
	h := &api.Handler{Systems: systems, Log: slog.New(slog.DiscardHandler)}
	res, err := h.ListSystems(context.Background(), connect.NewRequest(&v1.ListSystemsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Msg.GetSystems(); got[0].GetError() != "" || got[1].GetError() == "" {
		t.Errorf("errors %q, %q", got[0].GetError(), got[1].GetError())
	}
}

// A caller that goes away is not a failed summary.
func TestListSystemsCancelIsNotLogged(t *testing.T) {
	systems, a, _ := twoSystems()
	systems[0].Provider = summaryOf{a, "hang"}
	var log strings.Builder
	h := &api.Handler{Systems: systems, Log: slog.New(slog.NewTextHandler(&log, nil))}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	if _, err := h.ListSystems(ctx, connect.NewRequest(&v1.ListSystemsRequest{})); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(log.String(), "summary failed") {
		t.Errorf("a cancel was logged:\n%s", log.String())
	}
}

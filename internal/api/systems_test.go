package api_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// renamed is the fake scenario under another cluster name; it counts the
// calls that reach it.
type renamed struct {
	*fake.Provider
	cluster string
	calls   *atomic.Int32
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
func (r renamed) WriteMetrics(_ context.Context, m *api.Exposition) error {
	r.calls.Add(1)
	m.Family("nats_lens_test_up", "gauge", "Test family without labels.")
	m.Sample("nats_lens_test_up", 1)
	m.Family("nats_lens_test_by_code", "gauge", "Test family with a label.")
	m.Sample("nats_lens_test_by_code", 2, "code", r.cluster+"-001")
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
		a.GetStreams() != 9 || a.GetConsumers() != 54 {
		t.Errorf("account = %v", a)
	}
}

func TestBucketListsAreEmpty(t *testing.T) {
	_, c := apitest.Serve(t, fake.New(false))
	kv, err := c.ListKeyValueStores(context.Background(), connect.NewRequest(&v1.ListKeyValueStoresRequest{}))
	if err != nil || len(kv.Msg.GetKeyValueStores()) != 0 {
		t.Errorf("key-value stores = %v, %v", kv, err)
	}
	obj, err := c.ListObjectStores(context.Background(), connect.NewRequest(&v1.ListObjectStoresRequest{}))
	if err != nil || len(obj.Msg.GetObjectStores()) != 0 {
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
	want := `# HELP nats_lens_test_up Test family without labels.
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

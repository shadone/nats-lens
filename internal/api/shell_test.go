package api_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// shellCounted counts the ShellStatus and SystemSummary calls that reach
// a provider and keeps the last ShellStatus range; fail makes its
// SystemSummary fail.
type shellCounted struct {
	renamed
	shell, summaries *atomic.Int32
	fail             bool
	mu               *sync.Mutex
	lastRange        **v1.TimeRange
}

func newShellCounted(cluster string) shellCounted {
	var r *v1.TimeRange
	return shellCounted{renamed: newRenamed(cluster), shell: &atomic.Int32{}, summaries: &atomic.Int32{}, mu: &sync.Mutex{}, lastRange: &r}
}

func (s shellCounted) ShellStatus(ctx context.Context, req *v1.GetShellStatusRequest) (*v1.GetShellStatusResponse, error) {
	s.shell.Add(1)
	s.mu.Lock()
	*s.lastRange = proto.CloneOf(req.GetRange())
	s.mu.Unlock()
	return s.renamed.ShellStatus(ctx, req)
}

func (s shellCounted) SystemSummary(ctx context.Context, req *v1.ListSystemsRequest) (*v1.SystemSummary, error) {
	s.summaries.Add(1)
	if s.fail {
		return nil, errors.New("database is gone")
	}
	return s.renamed.SystemSummary(ctx, req)
}

func shellSystems() (api.Systems, shellCounted, shellCounted) {
	a, b := newShellCounted("cluster-a"), newShellCounted("cluster-b")
	return api.Systems{
		{Ref: &v1.SystemRef{Id: "a", Name: "System A", Environment: "production"}, Provider: a},
		{Ref: &v1.SystemRef{Id: "b", Name: "System B"}, Provider: b},
	}, a, b
}

func shellStatus(t *testing.T, c interface {
	GetShellStatus(context.Context, *connect.Request[v1.GetShellStatusRequest]) (*connect.Response[v1.GetShellStatusResponse], error)
}, req *v1.GetShellStatusRequest) *v1.GetShellStatusResponse {
	t.Helper()
	res, err := c.GetShellStatus(context.Background(), connect.NewRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg
}

// An unknown system is not an error: the systems come back with
// system_known false, and no provider is asked for the system's part.
func TestShellStatusUnknownSystem(t *testing.T) {
	systems, a, b := shellSystems()
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: systems})
	m := shellStatus(t, c, &v1.GetShellStatusRequest{System: "nope"})
	if m.GetSystemKnown() || m.GetDefaultSystem() != "a" || len(m.GetSystems()) != 2 {
		t.Fatalf("unknown system: known=%v default=%q systems=%v", m.GetSystemKnown(), m.GetDefaultSystem(), m.GetSystems())
	}
	if m.GetFreshness() != nil || m.GetServers() != nil || m.GetTally() != nil {
		t.Errorf("an unknown system has a part: %v", m)
	}
	if n := a.shell.Load() + b.shell.Load() + a.calls.Load() + b.calls.Load(); n != 0 {
		t.Errorf("%d provider calls for the unknown system's part", n)
	}
	for i, id := range []string{"a", "b"} {
		if got := m.GetSystems()[i].GetSystem().GetId(); got != id {
			t.Errorf("system %d is %q, want %q", i, got, id)
		}
	}
}

// The default and a named system get their part, read from their own
// provider over the last two collect intervals when the range is unset.
func TestShellStatusRoutesAndDefaultsTheRange(t *testing.T) {
	systems, a, b := shellSystems()
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: systems})
	for _, tc := range []struct {
		system string
		p      shellCounted
	}{{"", a}, {"a", a}, {"b", b}} {
		m := shellStatus(t, c, &v1.GetShellStatusRequest{System: tc.system})
		if !m.GetSystemKnown() || m.GetFreshness() == nil || m.GetTally() == nil {
			t.Fatalf("system %q: %v", tc.system, m)
		}
		tc.p.mu.Lock()
		r := *tc.p.lastRange
		tc.p.mu.Unlock()
		if span := r.GetEnd().AsTime().Sub(r.GetStart().AsTime()); span != 2*tc.p.CollectInterval() {
			t.Errorf("system %q: default range spans %v, want two intervals", tc.system, span)
		}
	}
	if a.shell.Load() != 2 || b.shell.Load() != 1 {
		t.Errorf("ShellStatus calls a=%d b=%d, want 2 and 1", a.shell.Load(), b.shell.Load())
	}
	if _, err := c.GetShellStatus(context.Background(), connect.NewRequest(&v1.GetShellStatusRequest{Range: &v1.TimeRange{
		Start: timestamppb.New(time.Now()), End: timestamppb.New(time.Now().Add(-time.Hour)),
	}})); codeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("a reversed range: %v", err)
	}
}

// The system's part is GetOverview's freshness, servers and tally over the
// same range, and each badge is the status and collection ListSystems
// reports over its default range.
func TestShellStatusAgreesWithOverviewAndListSystems(t *testing.T) {
	systems, _, _ := shellSystems()
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: systems})
	ctx := context.Background()
	now := systems[1].Provider.Now()
	r := &v1.TimeRange{Start: timestamppb.New(now.Add(-time.Hour)), End: timestamppb.New(now)}
	m := shellStatus(t, c, &v1.GetShellStatusRequest{System: "b", Range: r})
	o, err := c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{System: "b", Range: r}))
	if err != nil {
		t.Fatal(err)
	}
	if d := apitest.ShellDiff(m, o.Msg); d != "" {
		t.Errorf("the shell differs from GetOverview: %s", d)
	}
	ls, err := c.ListSystems(ctx, connect.NewRequest(&v1.ListSystemsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range ls.Msg.GetSystems() {
		b := m.GetSystems()[i]
		if b.GetSystem().GetId() != s.GetSystem().GetId() || b.GetStatus() != s.GetStatus() || b.GetCollection() != s.GetCollection() || b.GetError() != "" {
			t.Errorf("badge %v, ListSystems %s status %v collection %v", b, s.GetSystem().GetId(), s.GetStatus(), s.GetCollection())
		}
		if !b.GetCollecting() {
			t.Errorf("badge %s does not collect", b.GetSystem().GetId())
		}
	}
}

// A badge summary is read once per collect interval; a failed one is not
// kept, and the runtime's collection status applies on every call.
func TestShellStatusKeepsBadgesForAnInterval(t *testing.T) {
	systems, a, b := shellSystems()
	b.fail = true
	systems[1].Provider = b
	var st atomic.Value
	st.Store(api.CollectionStatus{State: api.CollectionCollecting})
	systems[0].Status = func() api.CollectionStatus { return st.Load().(api.CollectionStatus) }
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: systems})
	first := shellStatus(t, c, &v1.GetShellStatusRequest{})
	if !first.GetSystems()[0].GetCollecting() || first.GetSystems()[1].GetError() == "" {
		t.Fatalf("first call: %v", first.GetSystems())
	}
	st.Store(api.CollectionStatus{State: api.CollectionNotConnected, LastError: "no servers", Since: time.Now()})
	second := shellStatus(t, c, &v1.GetShellStatusRequest{})
	if a.summaries.Load() != 1 {
		t.Errorf("system a's summary was read %d times in one interval, want once", a.summaries.Load())
	}
	if b.summaries.Load() != 2 {
		t.Errorf("system b's failed summary was read %d times, want twice", b.summaries.Load())
	}
	if s := second.GetSystems()[0]; s.GetCollecting() || s.GetCollection() != v1.CollectionState_COLLECTION_STATE_NOT_CONNECTED {
		t.Errorf("the kept badge ignores the runtime's status: %v", s)
	}
}

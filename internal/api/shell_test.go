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
// a provider, and the SystemSummary calls that returned, and keeps the
// last ShellStatus range; fail makes its SystemSummary fail.
type shellCounted struct {
	renamed
	shell, summaries, summarized *atomic.Int32
	fail                         bool
	// shellErr fails ShellStatus; shellHang blocks it until its context ends.
	shellErr  error
	shellHang bool
	// delay holds SystemSummary back, in nanoseconds.
	delay     *atomic.Int64
	mu        *sync.Mutex
	lastRange **v1.TimeRange
}

func newShellCounted(cluster string) shellCounted {
	var r *v1.TimeRange
	return shellCounted{renamed: newRenamed(cluster), shell: &atomic.Int32{}, summaries: &atomic.Int32{}, summarized: &atomic.Int32{}, mu: &sync.Mutex{}, lastRange: &r, delay: &atomic.Int64{}}
}

func (s shellCounted) ShellStatus(ctx context.Context, req *v1.GetShellStatusRequest) (*v1.GetShellStatusResponse, error) {
	s.shell.Add(1)
	s.mu.Lock()
	*s.lastRange = proto.CloneOf(req.GetRange())
	s.mu.Unlock()
	if s.shellHang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if s.shellErr != nil {
		return nil, s.shellErr
	}
	return s.renamed.ShellStatus(ctx, req)
}

func (s shellCounted) SystemSummary(ctx context.Context, req *v1.ListSystemsRequest) (*v1.SystemSummary, error) {
	s.summaries.Add(1)
	defer s.summarized.Add(1)
	if d := time.Duration(s.delay.Load()); d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
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

// A system whose part fails or runs past ShellTimeout still gets the
// systems, their badges and the default; the failure is in the response's
// error, not the call's.
func TestShellStatusPartFailureKeepsTheSystems(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		set        func(*shellCounted)
	}{
		{"error", "internal", func(s *shellCounted) { s.shellErr = errors.New("database is gone") }},
		{"timeout", "deadline_exceeded", func(s *shellCounted) { s.shellHang = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			systems, a, _ := shellSystems()
			tc.set(&a)
			systems[0].Provider = a
			_, c := apitest.ServeHandler(t, &api.Handler{Systems: systems, ShellTimeout: 200 * time.Millisecond})
			start := time.Now()
			m := shellStatus(t, c, &v1.GetShellStatusRequest{System: "a"})
			if took := time.Since(start); took > time.Second {
				t.Errorf("the call took %v; the part's limit is 200ms", took)
			}
			if m.GetErrorCode() != tc.code || m.GetError() == "" || !m.GetSystemKnown() || m.GetDefaultSystem() != "a" || len(m.GetSystems()) != 2 {
				t.Errorf("code %q error %q known %v default %q systems %d", m.GetErrorCode(), m.GetError(), m.GetSystemKnown(), m.GetDefaultSystem(), len(m.GetSystems()))
			}
			if m.GetFreshness() != nil || m.GetTally() != nil {
				t.Errorf("a failed part has data: %v", m)
			}
			if tc.code == "internal" && m.GetError() == "database is gone" {
				t.Error("the provider's own text reached the client")
			}
		})
	}
}

// eventually waits up to ten seconds for cond.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !cond(); {
		if time.Now().After(deadline) {
			t.Fatalf("never: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// An expired badge is served at once while one read in the background
// replaces it; calls during that read start no other.
func TestShellStatusServesAnExpiredBadgeWhileItIsRead(t *testing.T) {
	systems, a, _ := shellSystems()
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	h := &api.Handler{Systems: systems}
	api.SetBadgeClock(h, func() time.Time { return time.Unix(0, clock.Load()) }, 0)
	_, c := apitest.ServeHandler(t, h)
	shellStatus(t, c, &v1.GetShellStatusRequest{System: "b"})
	clock.Add(int64(31 * time.Second))
	a.delay.Store(int64(time.Second))
	for range 3 {
		start := time.Now()
		m := shellStatus(t, c, &v1.GetShellStatusRequest{System: "b"})
		if took := time.Since(start); took > 400*time.Millisecond {
			t.Errorf("the call waited %v for an expired badge", took)
		}
		if s := m.GetSystems()[0]; s.GetPending() || s.GetStatus() == v1.Status_STATUS_UNSPECIFIED {
			t.Errorf("the expired badge was not served: %v", s)
		}
	}
	// Once the background read has ended, its badge is fresh. Waiting for
	// the read rather than a fixed time: a loaded machine can start it late.
	eventually(t, "the background read ends", func() bool { return a.summarized.Load() == 2 })
	if n := a.summaries.Load(); n != 2 {
		t.Errorf("system a's summary was read %d times, want once more in the background", n)
	}
	shellStatus(t, c, &v1.GetShellStatusRequest{System: "b"})
	if n := a.summaries.Load(); n != 2 {
		t.Errorf("a fresh badge was read again: %d reads", n)
	}
}

// A system with no badge kept whose first read is slow is pending; the
// other systems and the page system's part do not wait for it, and a later
// call has it without reading it again.
func TestShellStatusPendingBadge(t *testing.T) {
	systems, _, b := shellSystems()
	b.delay.Store(int64(800 * time.Millisecond))
	h := &api.Handler{Systems: systems}
	api.SetBadgeClock(h, nil, 100*time.Millisecond)
	_, c := apitest.ServeHandler(t, h)
	start := time.Now()
	m := shellStatus(t, c, &v1.GetShellStatusRequest{System: "a"})
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Errorf("the call took %v", took)
	}
	if !m.GetSystems()[1].GetPending() || m.GetSystems()[0].GetPending() || m.GetFreshness() == nil {
		t.Fatalf("badges %v, freshness %v", m.GetSystems(), m.GetFreshness())
	}
	eventually(t, "the slow badge arrives", func() bool {
		return !shellStatus(t, c, &v1.GetShellStatusRequest{System: "a"}).GetSystems()[1].GetPending()
	})
	if n := b.summaries.Load(); n != 1 {
		t.Errorf("the slow badge was read %d times, want once", n)
	}
}

// A runtime status that panics on a kept badge gives that badge an error.
func TestShellStatusBadgePanic(t *testing.T) {
	systems, _, _ := shellSystems()
	var boom atomic.Bool
	systems[1].Status = func() api.CollectionStatus {
		if boom.Load() {
			panic("status exploded")
		}
		return api.CollectionStatus{State: api.CollectionCollecting}
	}
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: systems})
	shellStatus(t, c, &v1.GetShellStatusRequest{})
	boom.Store(true)
	m := shellStatus(t, c, &v1.GetShellStatusRequest{})
	if m.GetSystems()[1].GetError() == "" || m.GetSystems()[0].GetError() != "" {
		t.Errorf("badges after a panic: %v", m.GetSystems())
	}
}

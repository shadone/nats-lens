package api_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// replayStub answers Replay with the request it got and the rest as the
// demo.
type replayStub struct {
	*fake.Provider
	got *v1.GetReplayRequest
}

func (s *replayStub) Replay(_ context.Context, req *v1.GetReplayRequest) (*api.ReplayFacts, error) {
	s.got = req
	return &api.ReplayFacts{A: req.GetA().AsTime(), B: req.GetB().AsTime(), Interval: 30 * time.Second}, nil
}

func replayRequest(a, b time.Time) *connect.Request[v1.GetReplayRequest] {
	r := &v1.GetReplayRequest{}
	if !a.IsZero() {
		r.A = timestamppb.New(a)
	}
	if !b.IsZero() {
		r.B = timestamppb.New(b)
	}
	return connect.NewRequest(r)
}

func TestGetReplayValidates(t *testing.T) {
	stub := &replayStub{Provider: fake.New(false)}
	_, c := apitest.Serve(t, stub)
	now := fake.FixedNow
	for name, tc := range map[string]struct {
		a, b time.Time
		want string
	}{
		"a missing":         {time.Time{}, now, "a and b are required"},
		"b missing":         {now.Add(-time.Hour), time.Time{}, "a and b are required"},
		"a after b":         {now.Add(-time.Hour), now.Add(-2 * time.Hour), "a must be before b"},
		"a equals b":        {now.Add(-time.Hour), now.Add(-time.Hour), "a must be before b"},
		"a after now":       {now.Add(time.Hour), now.Add(2 * time.Hour), "a must be before b, and before now"},
		"more than 24h":     {now.Add(-25 * time.Hour), now.Add(-time.Minute), "at most 24 hours apart"},
		"invalid timestamp": {time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), now, "a:"},
	} {
		stub.got = nil
		_, err := c.GetReplay(context.Background(), replayRequest(tc.a, tc.b))
		if connect.CodeOf(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want invalid_argument %q", name, err, tc.want)
		}
		if stub.got != nil {
			t.Errorf("%s: the provider was called", name)
		}
	}
}

func TestGetReplayClampsBToNow(t *testing.T) {
	stub := &replayStub{Provider: fake.New(false)}
	_, c := apitest.Serve(t, stub)
	now := fake.FixedNow
	res, err := c.GetReplay(context.Background(), replayRequest(now.Add(-time.Hour), now.Add(3*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if !stub.got.GetB().AsTime().Equal(now) || !res.Msg.GetB().AsTime().Equal(now) {
		t.Errorf("b = %v, response b = %v, want now", stub.got.GetB().AsTime(), res.Msg.GetB().AsTime())
	}
	// 24 hours up to a B after now is allowed once B is clamped.
	if _, err := c.GetReplay(context.Background(), replayRequest(now.Add(-24*time.Hour), now.Add(time.Hour))); err != nil {
		t.Errorf("24 hours to a clamped B: %v", err)
	}
}

func TestGetReplayRefusesAOlderThanRetention(t *testing.T) {
	_, c := apitest.Serve(t, fake.New(false))
	a := fake.FixedNow.Add(-31 * 24 * time.Hour)
	_, err := c.GetReplay(context.Background(), replayRequest(a, a.Add(time.Hour)))
	if connect.CodeOf(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), "A is older than retention (30d)") {
		t.Errorf("err = %v", err)
	}
}

// The demo tells example-prod's node upgrade: the drain, the three
// restarts and the quorum losses, every step with its evidence.
func TestGetReplayDemo(t *testing.T) {
	_, c := apitest.Serve(t, fake.New(false))
	at := func(h, m int) time.Time { return time.Date(2026, 10, 17, h, m, 0, 0, time.UTC) }
	res, err := c.GetReplay(context.Background(), replayRequest(at(9, 10), at(9, 55)))
	if err != nil {
		t.Fatal(err)
	}
	r := res.Msg
	restarts := 0
	var titles []string
	for _, s := range r.GetSteps() {
		titles = append(titles, s.GetTitle())
		if s.GetKind() == v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART {
			restarts++
		}
		if s.GetEvidenceCount() == 0 {
			t.Errorf("step %q has no evidence", s.GetTitle())
		}
	}
	if restarts != 3 || r.GetCounts().GetServersRestarted() != 3 || r.GetEpochA() == 0 || r.GetEpochB() == 0 || len(r.GetWindows()) == 0 ||
		!r.GetKubernetesWatched() {
		t.Errorf("restarts %d, counts %v, epochs %d %d, windows %v\n%s", restarts, r.GetCounts(), r.GetEpochA(), r.GetEpochB(), r.GetWindows(),
			strings.Join(titles, "\n"))
	}
	if !strings.Contains(strings.Join(titles, "\n"), "Node gke-example-pool-1-7f3a") &&
		!strings.Contains(strings.Join(titles, "\n"), "node gke-example-pool-1-7f3a cordoned and drained") {
		t.Errorf("no drain step:\n%s", strings.Join(titles, "\n"))
	}
}

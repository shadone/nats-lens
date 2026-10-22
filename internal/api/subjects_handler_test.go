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

// subjectsStub answers Subjects and MatchSubject with its facts and the
// rest as the demo; it names the account APP.
type subjectsStub struct {
	*fake.Provider
	calls   int
	left    time.Duration
	subReq  *v1.GetSubjectsRequest
	facts   *api.SubjectsFacts
	matchIn *v1.MatchSubjectRequest
	match   *api.MatchFacts
}

func (s *subjectsStub) Subjects(ctx context.Context, req *v1.GetSubjectsRequest) (*api.SubjectsFacts, error) {
	s.calls++
	d, _ := ctx.Deadline()
	s.left, s.subReq = time.Until(d), req
	return s.facts, nil
}

func (s *subjectsStub) MatchSubject(_ context.Context, req *v1.MatchSubjectRequest) (*api.MatchFacts, error) {
	s.calls++
	s.matchIn = req
	return s.match, nil
}

func (s *subjectsStub) AccountNames(context.Context) (map[string]string, error) {
	return map[string]string{"APP": "shop"}, nil
}

// The handler fills the range, calls the provider within QueryTimeout,
// finishes the facts with the request's scope and filter and names the
// accounts; without an epoch the response is empty.
func TestSubjectsHandler(t *testing.T) {
	p := &subjectsStub{Provider: fake.New(false), facts: subjectsFacts()}
	_, c := apitest.Serve(t, p)
	res, err := c.GetSubjects(context.Background(), connect.NewRequest(&v1.GetSubjectsRequest{Account: "APP",
		Filter: v1.SubjectFilter_SUBJECT_FILTER_WIDE}))
	if err != nil {
		t.Fatal(err)
	}
	if p.left > api.QueryTimeout || p.left < api.QueryTimeout-time.Second {
		t.Errorf("provider had %s, want %s", p.left, api.QueryTimeout)
	}
	if r := p.subReq.GetRange(); r.GetStart() == nil || r.GetEnd() == nil || r.GetEnd().AsTime().Sub(r.GetStart().AsTime()) != api.DefaultRange {
		t.Errorf("range = %v, want the default span with both ends", r)
	}
	m := res.Msg
	if m.GetEpoch() != 10 || len(m.GetRows()) != 2 || m.GetAllCount() != 15 || m.GetRows()[0].GetAccountName() != "shop" {
		t.Errorf("response = %v", m)
	}
	p.facts = &api.SubjectsFacts{}
	res, err = c.GetSubjects(context.Background(), connect.NewRequest(&v1.GetSubjectsRequest{}))
	if err != nil || res.Msg.GetEpoch() != 0 || res.Msg.GetEpochAt() != nil || len(res.Msg.GetRows()) != 0 {
		t.Errorf("no epoch: %v, %v", res, err)
	}
}

// MatchSubject refuses a subject that is not a literal before any
// provider call, and finishes the facts for a valid one.
func TestMatchSubjectHandler(t *testing.T) {
	f := subjectsFacts()
	p := &subjectsStub{Provider: fake.New(false), match: &api.MatchFacts{Epoch: 10, EpochAt: subjAt, Streams: f.Streams}}
	_, c := apitest.Serve(t, p)
	for _, bad := range []string{"", "orders.*", "orders.>", "orders..x", "orders x", strings.Repeat("a", 256)} {
		_, err := c.MatchSubject(context.Background(), connect.NewRequest(&v1.MatchSubjectRequest{Subject: bad}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%q: %v, want invalid_argument", bad, err)
		}
	}
	_, err := c.MatchSubject(context.Background(), connect.NewRequest(&v1.MatchSubjectRequest{Subject: "orders.x",
		Range: &v1.TimeRange{Start: timestamppb.New(fake.FixedNow), End: timestamppb.New(fake.FixedNow.Add(-time.Hour))}}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("inverted range: %v", err)
	}
	if p.calls != 0 {
		t.Errorf("%d provider calls for invalid requests", p.calls)
	}
	res, err := c.MatchSubject(context.Background(), connect.NewRequest(&v1.MatchSubjectRequest{Account: "APP", Subject: "orders.place"}))
	if err != nil {
		t.Fatal(err)
	}
	if p.matchIn.GetRange().GetEnd() == nil || len(api.MatchedStreams(res.Msg)) != 1 || res.Msg.GetAccounts()[0].GetAccountName() != "shop" {
		t.Errorf("response = %v", res.Msg)
	}
}

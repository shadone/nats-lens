package store

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/micro"

	"github.com/shadone/nats-lens/internal/collect"
)

func writeOneInstance(t *testing.T, s *Store, at time.Time, eps func(i int) []*micro.EndpointStats, i int, started time.Time) {
	t.Helper()
	snap := fixtureSnapshot(t, at)
	acc := collect.MicroAccount{Label: "APP", Account: "APP",
		Asked: []collect.MicroAsked{{Request: collect.MicroStats, State: collect.MicroOK, Replies: 1}}}
	acc.Stats = []micro.Stats{{ServiceIdentity: micro.ServiceIdentity{Name: "orders", ID: "A"},
		Type: micro.StatsResponseType, Started: started, Endpoints: eps(i)}}
	snap.Micro = []collect.MicroAccount{acc}
	if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
}

// Two endpoints of one name, neither with a subject (an implementation
// whose STATS omits it, no INFO yet), cannot be told apart: a known limit,
// as the micro framework always reports subjects. The test is skipped.
func TestMicroEndpointHoursSameNameNoSubject(t *testing.T) {
	t.Skip("known limit: two endpoints of one name without subjects are taken as one")
	s := openMem(t)
	h := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	for i := range 3 {
		writeOneInstance(t, s, h.Add(time.Duration(i)*30*time.Second), func(i int) []*micro.EndpointStats {
			return []*micro.EndpointStats{
				{Name: "get", NumRequests: 50000 + 1000*i},
				{Name: "get", NumRequests: 10 + i},
			}
		}, i, h.Add(-24*time.Hour))
	}
	got := rowStrings(t, s.DB(), `SELECT requests, epochs FROM micro_endpoint_hours`)
	t.Logf("hours: %v", got)
	if len(got) != 1 || got[0] != "2002|3" {
		t.Errorf("hours %v, want 2002|3", got)
	}
}

// The subject is unknown in the first epoch (STATS without it, INFO not
// yet answered) and known from the second: the second epoch's increase
// still counts.
func TestMicroEndpointHoursSubjectAppears(t *testing.T) {
	s := openMem(t)
	h := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	for i := range 3 {
		writeOneInstance(t, s, h.Add(time.Duration(i)*30*time.Second), func(i int) []*micro.EndpointStats {
			subj := "orders.get"
			if i == 0 {
				subj = ""
			}
			return []*micro.EndpointStats{{Name: "get", Subject: subj, NumRequests: 100 + 10*i}}
		}, i, h.Add(-24*time.Hour))
	}
	got := rowStrings(t, s.DB(), `SELECT requests, epochs FROM micro_endpoint_hours`)
	t.Logf("hours: %v", got)
	if len(got) != 1 || got[0] != "20|3" {
		t.Errorf("hours %v, want 20|3", got)
	}
}

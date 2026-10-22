package store

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go/micro"
)

// An endpoint's subject is set, then missing, then set again: each row
// pairs with the newest previous one, 10 + 10 = 20.
func TestMicroEndpointHoursSubjectFlaps(t *testing.T) {
	s := openMem(t)
	h := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	subs := []string{"orders.get", "", "orders.get"}
	for i := range 3 {
		writeOneInstance(t, s, h.Add(time.Duration(i)*30*time.Second), func(i int) []*micro.EndpointStats {
			return []*micro.EndpointStats{{Name: "get", Subject: subs[i], NumRequests: 100 + 10*i}}
		}, i, h.Add(-24*time.Hour))
	}
	got := rowStrings(t, s.DB(), `SELECT requests, epochs FROM micro_endpoint_hours`)
	if len(got) != 1 || got[0] != "20|3" {
		t.Errorf("hours %v, want 20|3", got)
	}
}

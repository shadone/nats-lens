package store

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/collect"
)

// A held request is stored with state held, the server's refusal as
// received in error, and refused_at and retry_after; the other states
// leave both NULL. The times are the same whatever the database's time
// zone.
func TestMicroHeldRequestStored(t *testing.T) {
	refused := time.Date(2026, 10, 16, 21, 55, 30, 0, time.UTC)
	text := `nats: permissions violation: Permissions Violation for Publish to "$SRV.STATS"`
	for _, zone := range []string{"UTC", "America/Los_Angeles", "Pacific/Auckland"} {
		s := openMem(t)
		if _, err := s.DB().Exec(`SET GLOBAL TimeZone = '` + zone + `'`); err != nil {
			t.Fatal(err)
		}
		snap := fixtureSnapshot(t, t0)
		snap.Micro[1].Asked = []collect.MicroAsked{
			{Request: collect.MicroStats, State: collect.MicroHeld, Error: text, RefusedAt: refused, RetryAfter: refused.Add(10 * time.Minute)},
		}
		epoch, err := s.WriteSnapshot(context.Background(), snap)
		if err != nil {
			t.Fatal(err)
		}
		wantRows(t, zone, rowStrings(t, s.DB(), `SELECT label, request, state, replies, error,
  strftime(refused_at, '%Y-%m-%d %H:%M:%S'), strftime(retry_after, '%H:%M:%S')
FROM micro_requests WHERE epoch = ? ORDER BY label, request`, epoch), []string{
			"APP|INFO|ok|2|NULL|NULL|NULL",
			"APP|STATS|ok|2|NULL|NULL|NULL",
			"OPS|STATS|held|0|" + text + "|2026-10-16 21:55:30|22:05:30",
		})
	}
}

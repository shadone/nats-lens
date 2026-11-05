package store

import (
	"context"
	"testing"
	"time"
)

// A mute of the system entity is stored with an empty key, as the system
// finding has, and covers that finding; it is not refused by the table's
// rule that the entity type and key are both set or both NULL.
func TestMuteOfTheSystemEntity(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	addEpoch(t, s, 1, t0)
	apply(t, s, 1, CheckRun{Code: "system-002", EntityType: "system", Status: RunOK, Rows: []FindingRow{frow("", "critical", "no leader")}})
	m, err := s.CreateMute(ctx, Mute{CheckCode: "system-002", EntityType: "system", Reason: "maintenance", MutedBy: "op",
		CreatedAt: t0, ExpiresAt: t0.Add(time.Hour)})
	if err != nil {
		t.Fatalf("mute of the system entity: %v", err)
	}
	if m.EntityType != "system" || m.EntityKey != "" {
		t.Fatalf("mute = %+v, want entity system with no key", m)
	}
	fs, err := s.Findings(ctx, t0.Add(time.Minute), FindingQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Mute == nil || fs[0].Mute.ID != m.ID {
		t.Fatalf("findings = %+v, want system-002 muted by %d", fs, m.ID)
	}
	all, _, err := s.ListMutes(ctx, t0.Add(time.Minute), false, 10, 0)
	if err != nil || len(all) != 1 || all[0].EntityType != "system" {
		t.Fatalf("mutes = %+v, %v", all, err)
	}
}

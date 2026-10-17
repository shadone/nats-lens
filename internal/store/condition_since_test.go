package store

import (
	"context"
	"testing"
	"time"
)

// A finding keeps the time its condition began, as the check gave it,
// apart from since, when nats-lens first saw it; a later run updates it.
func TestFindingConditionSince(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	addEpoch(t, s, 1, t0)
	addEpoch(t, s, 2, t0.Add(30*time.Second))
	began := t0.Add(-48 * time.Hour)
	row := frow("APP/jobs", "info", "created differs")
	row.ConditionSince = &began
	apply(t, s, 1, okRun("stream-008", row))
	later := t0.Add(-time.Hour)
	row.ConditionSince = &later
	apply(t, s, 2, okRun("stream-008", row))
	fs, err := s.Findings(ctx, t0.Add(time.Minute), FindingQuery{})
	if err != nil || len(fs) != 1 {
		t.Fatalf("findings %v %v", fs, err)
	}
	if f := fs[0]; f.ConditionSince == nil || !f.ConditionSince.Equal(later) || !f.Since.Equal(t0) {
		t.Fatalf("condition since %v, since %v", f.ConditionSince, f.Since)
	}
}

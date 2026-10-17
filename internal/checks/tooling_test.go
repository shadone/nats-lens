package checks

import (
	"context"
	"testing"
)

// The guard stops above 60% and returns only below 40%; an epoch with
// fewer than three non-own connections keeps the previous decision.
func TestNextLoopbackHysteresis(t *testing.T) {
	for _, c := range []struct {
		prev                     bool
		loop, others, epochOther int64
		want                     bool
	}{
		{true, 61, 100, 10, false},
		{true, 59, 100, 10, true},
		{false, 50, 100, 10, false},
		{false, 39, 100, 10, true},
		{false, 90, 100, 2, false},
		{true, 90, 100, 2, true},
		{true, 0, 0, 0, true},
	} {
		if got := nextLoopback(c.prev, c.loop, c.others, c.epochOther); got != c.want {
			t.Errorf("%+v: got %v", c, got)
		}
	}
}

// A thin epoch between two does not reset a guard that is off: off, then
// an epoch with two connections, then 50% stays off.
func TestNextLoopbackThinEpochKeepsTheGuard(t *testing.T) {
	on := nextLoopback(true, 70, 100, 10)
	on = nextLoopback(on, 90, 100, 2)
	on = nextLoopback(on, 50, 100, 10)
	if on {
		t.Error("a thin epoch turned the guard back on")
	}
}

// An epoch without a stored decision (its checks failed or were skipped)
// does not reset the guard: the next decision starts from the latest one
// stored in the trailing window.
func TestLoopbackDecisionSkipsAnUndecidedEpoch(t *testing.T) {
	f := newFx(t)
	f.cluster(3)
	for e := int64(1); e <= 3; e++ {
		for i := int64(0); i < 10; i++ {
			ip := "10.0.0.9"
			if i < 5 {
				ip = "127.0.0.1"
			}
			f.row("connections", m{"epoch": e, "server_id": "S0", "cid": i + 1, "name": "x", "ip": ip})
		}
	}
	ctx := context.Background()
	if err := f.st.SetLoopbackTooling(ctx, 1, false); err != nil {
		t.Fatal(err)
	}
	// Epoch 2 has no decision; at 50% epoch 3 stays off.
	on, stored, err := LoopbackDecision(ctx, f.st.DB(), 3, nil)
	if err != nil || stored || on {
		t.Fatalf("epoch 3: on %v, stored %v, err %v; want off, computed", on, stored, err)
	}
}

// A decided epoch keeps its decision: a second store does not change it.
func TestSetLoopbackToolingKeepsTheStoredValue(t *testing.T) {
	f := newFx(t)
	f.cluster(1)
	ctx := context.Background()
	if err := f.st.SetLoopbackTooling(ctx, 1, false); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetLoopbackTooling(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	if on, err := StoredLoopback(ctx, f.st.DB(), 1); err != nil || on {
		t.Errorf("stored decision %v, %v; want the first, off", on, err)
	}
}

package collect

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func statsIDs(ms []MicroAccount) []string {
	var out []string
	for _, m := range ms {
		for _, s := range m.Stats {
			out = append(out, m.Label+":"+s.ID)
		}
	}
	return out
}

// A known instance answers 400 ms late every epoch and a new instance
// appears (a scale-up, not a restart): the slow one is still kept.
func TestMicroSlowKnownAndNewInstance(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	svc := connectAs(t, url, "shop")
	var near, far, newd atomic.Int64
	statsResponder(t, svc, "NEAR", &near)
	statsResponder(t, svc, "FAR", &far)
	at := time.Now()
	c := microCollector(3*time.Second, 5*time.Minute, &at, fixedConn("shop", connectAs(t, url, "shop")))
	c.Collect(context.Background())
	far.Store(int64(400 * time.Millisecond))
	for e := range 4 {
		at = at.Add(30 * time.Second)
		if e == 1 {
			statsResponder(t, svc, "NEW", &newd)
		}
		got := statsIDs(c.Collect(context.Background()).Micro)
		t.Logf("epoch %d: %v", e+2, got)
		if e >= 1 && len(got) != 3 {
			t.Errorf("epoch %d: FAR (known, 400 ms late) cut: %v", e+2, got)
		}
	}
}

// A rolling deploy gives one instance a new ID every epoch while a known
// one answers 350 ms late: the slow one is kept in every epoch.
func TestMicroRollingDeploySlowKnown(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	svc := connectAs(t, url, "shop")
	var fast, slow atomic.Int64
	statsResponder(t, svc, "SLOW", &slow)
	statsResponder(t, svc, "R0", &fast)
	at := time.Now()
	c := microCollector(3*time.Second, 5*time.Minute, &at, fixedConn("shop", connectAs(t, url, "shop")))
	c.Collect(context.Background())
	slow.Store(int64(350 * time.Millisecond))
	missing := 0
	for e := range 4 {
		at = at.Add(30 * time.Second)
		// a fresh connection per rolled instance
		statsResponder(t, connectAs(t, url, "shop"), "R"+string(rune('1'+e)), &fast)
		got := statsIDs(c.Collect(context.Background()).Micro)
		t.Logf("epoch %d: %v", e+2, got)
		if !strings.Contains(strings.Join(got, ","), "SLOW") {
			missing++
		}
	}
	if missing > 0 {
		t.Errorf("SLOW (known, 350 ms) missing in %d of 4 epochs of a rolling deploy", missing)
	}
}

// A known instance cut once, because a new instance capped the wait, is
// still waited for in the next epoch, when nothing new caps it.
func TestMicroCutInstanceStaysKnown(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	svc := connectAs(t, url, "shop")
	var near, far, newd atomic.Int64
	statsResponder(t, svc, "NEAR", &near)
	statsResponder(t, svc, "FAR", &far)
	at := time.Now()
	c := microCollector(3*time.Second, 5*time.Minute, &at, fixedConn("shop", connectAs(t, url, "shop")))
	c.Collect(context.Background())
	far.Store(int64(1800 * time.Millisecond))
	statsResponder(t, svc, "NEW", &newd)
	at = at.Add(30 * time.Second)
	if got := statsIDs(c.Collect(context.Background()).Micro); len(got) != 2 {
		t.Fatalf("epoch 2: FAR, 1.8 s late, not cut at the cap: %v", got)
	}
	at = at.Add(30 * time.Second)
	if got := statsIDs(c.Collect(context.Background()).Micro); len(got) != 3 {
		t.Errorf("epoch 3: FAR, cut once, no longer waited for: %v", got)
	}
}

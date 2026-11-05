package checks

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
)

func TestMirrorError(t *testing.T) {
	f := newFx(t)
	f.cluster(5)
	for _, s := range []string{"order-archive", "once", "fine", "keeps", "cleared", "alternate"} {
		f.streamConfig("S0", s, 1, 5, m{"mirror": "order-events"})
	}
	gone := m{"mirror_error": "stream not found"}
	for e := int64(1); e <= 5; e++ {
		f.streamMirrorAt(e, "order-archive", 500, time.Minute, gone)
		f.streamMirrorAt(e, "fine", 0, time.Second, nil)
		// A follower's copy of the error is not read.
		f.streamStats(e, "S1", "fine", m{"leader": "nats-0", "is_leader": false, "mirror_lag": 0, "mirror_error": "old"})
		// The server clears the error at each retry: it shows in every
		// other collection.
		if e%2 == 0 {
			f.streamMirrorAt(e, "alternate", 0, 5*time.Minute, gone)
		} else {
			f.streamMirrorAt(e, "alternate", 0, 5*time.Minute, nil)
		}
		// One collection with an error is a retry, not a finding.
		if e == 5 {
			f.streamMirrorAt(e, "once", 0, time.Second, gone)
		} else {
			f.streamMirrorAt(e, "once", 0, time.Second, nil)
		}
		// A firing finding stays while a report of the last four has the
		// error, and resolves after four without it.
		if e == 2 {
			f.streamMirrorAt(e, "keeps", 0, time.Second, gone)
		} else {
			f.streamMirrorAt(e, "keeps", 0, time.Second, nil)
		}
		if e == 1 {
			f.streamMirrorAt(e, "cleared", 0, time.Second, gone)
		} else {
			f.streamMirrorAt(e, "cleared", 0, time.Second, nil)
		}
	}
	f.streamFiringAt("stream-017", "APP/keeps", 4)
	f.streamFiringAt("stream-017", "APP/cleared", 4)
	r := f.fires("stream-017", 5, "APP/order-archive", "APP/alternate", "APP/keeps")
	want(t, r["APP/order-archive"], "warning", "Leader nats-0 reports the mirror of order-events failing: stream not found.")
	want(t, r["APP/alternate"], "warning", "failing: stream not found (not in the newest report; the server clears it when it tries again).")
	if r["APP/order-archive"].RelatedConfig["mirror"] != "order-events" {
		t.Errorf("related = %v", r["APP/order-archive"].RelatedConfig)
	}
	f.fires("stream-017", 1)
	if r := f.eval("stream-017", 5, map[string]string{"min_epochs": "1"}); len(r) != 3 {
		t.Fatalf("min_epochs 1: %s", rowsText(r))
	}
}

// While stream-017 reports the error, stream-015 is quiet for the same
// mirror, though it has not heard from its origin.
func TestMirrorErrorSuppressesLagging(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	f.streamConfig("S0", "order-archive", 1, 2, m{"mirror": "order-events"})
	f.streamConfig("S0", "media-copy", 1, 2, m{"mirror": "media-windows"})
	for e := int64(1); e <= 2; e++ {
		f.streamMirrorAt(e, "order-archive", 500, 5*time.Minute, m{"mirror_error": "stream not found"})
		f.streamMirrorAt(e, "media-copy", 0, 5*time.Minute, nil)
	}
	set, err := Load(config.Checks{Timeout: config.Duration(10 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Set: set, only: map[string]bool{"stream-015": true, "stream-017": true}}
	runs, err := e.Evaluate(context.Background(), f.st.DB(), 2)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, r := range runs {
		if r.Status != store.RunOK {
			t.Fatalf("%s: %s %s", r.Code, r.Status, r.Error)
		}
		for _, row := range r.Rows {
			got[r.Code] = append(got[r.Code], row.EntityKey)
		}
	}
	if len(got["stream-015"]) != 1 || got["stream-015"][0] != "APP/media-copy" {
		t.Errorf("stream-015 = %v, want APP/media-copy only", got["stream-015"])
	}
	if len(got["stream-017"]) != 1 || got["stream-017"][0] != "APP/order-archive" {
		t.Errorf("stream-017 = %v, want APP/order-archive only", got["stream-017"])
	}
}

// In the first collection of an error stream-017 waits for min_epochs;
// stream-015 does not fire meanwhile only to be suppressed in the next.
func TestMirrorErrorFirstCollection(t *testing.T) {
	f := newFx(t)
	f.cluster(12)
	f.streamConfig("S0", "order-archive", 1, 12, m{"mirror": "order-events"})
	for e := int64(1); e <= 12; e++ {
		if e < 11 {
			f.streamMirrorAt(e, "order-archive", 0, time.Second, nil)
		} else {
			f.streamMirrorAt(e, "order-archive", 0, 5*time.Minute, m{"mirror_error": "stream not found"})
		}
	}
	for _, e := range []int64{11, 12} {
		got := f.streamEvalWith(e, "stream-015", "stream-017")
		if len(got["stream-015"]) != 0 {
			t.Errorf("epoch %d: stream-015 = %s", e, rowsText(got["stream-015"]))
		}
	}
	if got := f.streamEvalWith(12, "stream-017"); len(got["stream-017"]) != 1 {
		t.Errorf("stream-017 = %s", rowsText(got["stream-017"]))
	}
}

// An error that shows in every other collection: stream-017 fires, and
// stream-015 stays quiet in the collections between.
func TestMirrorErrorAlternating(t *testing.T) {
	f := newFx(t)
	f.cluster(16)
	f.streamConfig("S0", "order-archive", 1, 16, m{"mirror": "order-evnts"})
	f.streamConfig("S0", "media-copy", 1, 16, m{"mirror": "media-windows"})
	for e := int64(1); e <= 16; e++ {
		cols := m{"leader_since": t0.Add(-24 * time.Hour)}
		if e%2 == 1 {
			cols["mirror_error"] = "stream not found"
		}
		f.streamMirrorAt(e, "order-archive", 0, -1, cols)
		// A mirror already firing for its lag when an error appears.
		mcols := m{}
		if e == 16 {
			mcols["mirror_error"] = "consumer create failed"
		}
		f.streamMirrorAt(e, "media-copy", 50000+e, time.Second, mcols)
	}
	f.streamFiringAt("stream-015", "APP/media-copy", 15)
	for e := int64(11); e <= 16; e++ {
		got := f.streamEvalWith(e, "stream-015", "stream-017")
		if _, ok := got["stream-015"]["APP/order-archive"]; ok {
			t.Errorf("epoch %d: stream-015 = %s", e, rowsText(got["stream-015"]))
		}
		if _, ok := got["stream-017"]["APP/order-archive"]; !ok {
			t.Errorf("epoch %d: stream-017 = %s", e, rowsText(got["stream-017"]))
		}
	}
	// stream-017 has not fired for media-copy yet: its lag finding stays.
	if got := f.streamEvalWith(16, "stream-015"); len(got["stream-015"]) != 1 {
		t.Errorf("media-copy: %s", rowsText(got["stream-015"]))
	}
	// Once stream-017 fires for it, the error rule takes it.
	f.streamFiringAt("stream-017", "APP/media-copy", 15)
	if got := f.streamEvalWith(16, "stream-015"); len(got["stream-015"]) != 0 {
		t.Errorf("media-copy with stream-017 firing: %s", rowsText(got["stream-015"]))
	}
}

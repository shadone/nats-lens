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
	f.cluster(3)
	for _, s := range []string{"order-archive", "once", "fine", "keeps", "cleared"} {
		f.streamConfig("S0", s, 1, 3, m{"mirror": "order-events"})
	}
	gone := m{"mirror_error": "stream not found"}
	for e := int64(1); e <= 3; e++ {
		f.streamMirrorAt(e, "order-archive", 500, time.Minute, gone)
		f.streamMirrorAt(e, "fine", 0, time.Second, nil)
		// A follower's copy of the error is not read.
		f.streamStats(e, "S1", "fine", m{"leader": "nats-0", "is_leader": false, "mirror_lag": 0, "mirror_error": "old"})
	}
	// One collection with an error is a retry, not a finding.
	f.streamMirrorAt(2, "once", 0, time.Second, nil)
	f.streamMirrorAt(3, "once", 0, time.Second, gone)
	// A firing finding stays while the newest report has the error, and
	// resolves on the first report without it.
	f.streamMirrorAt(2, "keeps", 0, time.Second, nil)
	f.streamMirrorAt(3, "keeps", 0, time.Second, gone)
	f.streamMirrorAt(2, "cleared", 0, time.Second, gone)
	f.streamMirrorAt(3, "cleared", 0, time.Second, nil)
	f.streamFiringAt("stream-017", "APP/keeps", 2)
	f.streamFiringAt("stream-017", "APP/cleared", 2)
	r := f.fires("stream-017", 3, "APP/order-archive", "APP/keeps")
	want(t, r["APP/order-archive"], "warning", "Leader nats-0 reports the mirror of order-events failing: stream not found.")
	if r["APP/order-archive"].RelatedConfig["mirror"] != "order-events" {
		t.Errorf("related = %v", r["APP/order-archive"].RelatedConfig)
	}
	f.fires("stream-017", 1)
	if r := f.eval("stream-017", 3, map[string]string{"min_epochs": "1"}); len(r) != 3 {
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

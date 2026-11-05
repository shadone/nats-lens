package checks

import (
	"testing"
	"time"
)

// jsapi stores, for each epoch from 1 to n, the API counters of server id
// growing by total and errors per collection.
func (f *fx) jsapi(n int64, id string, total, errors func(e int64) int64) {
	for e := int64(1); e <= n; e++ {
		f.stats(e, id, m{"js_api_total": 1000 + total(e), "js_api_errors": 10 + errors(e)})
	}
}

func per(k int64) func(int64) int64 { return func(e int64) int64 { return k * e } }

// Half the requests failing, and at least 100 errors in 15 minutes, in two
// collections in a row fire; a lower share or fewer errors do not.
func TestJetStreamAPIErrors(t *testing.T) {
	f := newFx(t)
	f.cluster(31)
	// 30 collections of 30 s are 15 minutes: at epoch 31 the window holds
	// the growth from epoch 1.
	f.jsapi(31, "S0", per(8), per(4))                               // 240 requests, 120 errors: 50%
	f.jsapi(31, "S1", per(10), per(4))                              // 300 requests, 120 errors: 40%
	f.jsapi(31, "S2", per(6), func(e int64) int64 { return 3 * e }) // 90 errors: too few
	r := f.fires("server-021", 31, "nats-0")
	want(t, r["nats-0"], "warning", "nats-0 answered 120 of 240 JetStream API requests (50%) with an error in the last 15m.")
	if v := r["nats-0"].Value; v == nil || *v != 0.5 {
		t.Errorf("value %v", v)
	}
}

// One collection over the thresholds is not enough.
func TestJetStreamAPIErrorsNeedTwoCollections(t *testing.T) {
	f := newFx(t)
	f.cluster(31)
	f.jsapi(30, "S0", per(8), per(1))
	f.stats(31, "S0", m{"js_api_total": 1000 + 8*31, "js_api_errors": 10 + 31 + 150})
	f.fires("server-021", 31)
}

// A restart starts the count again: the new process's counters are not
// compared with the old one's.
func TestJetStreamAPIErrorsRestart(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 31, three...)
	f.server("A0", "nats-0", 1, 29, t0.Add(-time.Hour), "2.15.0")
	f.server("B0", "nats-0", 30, 31, at(30), "2.15.0")
	f.jsapi(29, "A0", per(8), per(4))
	// The new process starts at 0: against the old counters its growth
	// would be negative; alone it has one collection before it.
	f.stats(30, "B0", m{"js_api_total": 200, "js_api_errors": 150})
	f.stats(31, "B0", m{"js_api_total": 400, "js_api_errors": 300})
	f.fires("server-021", 31)
}

// A firing finding stays while the errors hold 80% of both thresholds and
// resolves below.
func TestJetStreamAPIErrorsResolve(t *testing.T) {
	f := newFx(t)
	f.cluster(31)
	f.jsapi(31, "S0", per(10), func(e int64) int64 { return 4*e + e/31 }) // 121 of 300: 40.3%
	f.jsapi(31, "S1", per(10), func(e int64) int64 { return 4*e - e/31 }) // 119 of 300
	f.fires("server-021", 31)
	for _, name := range []string{"nats-0", "nats-1"} {
		f.row("findings", m{"id": "server-021:" + name, "check_code": "server-021", "entity_type": "server", "entity_key": name,
			"severity": "warning", "state": "firing", "last_transition": "firing", "since": at(20), "since_epoch": 20,
			"transitioned_at": at(20), "last_seen_epoch": 30, "updated_epoch": 30, "detail": "x"})
	}
	f.fires("server-021", 31, "nats-0")
}

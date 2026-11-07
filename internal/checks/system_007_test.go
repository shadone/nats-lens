package checks

import (
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

// failEndpoint records that server id left endpoint unanswered in epochs.
func (f *fx) failEndpoint(id, endpoint, state, msg string, epochs ...int64) {
	f.t.Helper()
	for _, e := range epochs {
		f.row("epoch_endpoints", m{"epoch": e, "server_id": id, "endpoint": endpoint, "state": state, "error": msg})
		f.exec(`UPDATE epochs SET partial = true, endpoint_failures = endpoint_failures + 1 WHERE epoch = ?`, e)
	}
}

func span(from, to int64) []int64 {
	var out []int64
	for e := from; e <= to; e++ {
		out = append(out, e)
	}
	return out
}

// The lookback of 30 minutes holds 61 collections at epoch 61; 25% of
// them is 15.25. RAFTZ unanswered by nats-1 in 16 and CONNZ failing once
// fires; RAFTZ unanswered in 15 alone does not, nor in 16 with the
// threshold at 0.3.
func TestCollectionsOftenIncomplete(t *testing.T) {
	f := newFx(t)
	f.cluster(61)
	f.failEndpoint("S1", "RAFTZ", "missing", "", span(46, 61)...)
	f.failEndpoint("S2", "CONNZ", "error", "permissions violation", 20)
	r := f.fires("system-007", 61, "")
	want(t, r[""], "warning", "17 of 61 collections in the last 30m were incomplete (28%): RAFTZ from nats-1 unanswered in 16; CONNZ from nats-2 failed in 1 (permissions violation).")
	g := newFx(t)
	g.cluster(61)
	g.failEndpoint("S1", "RAFTZ", "missing", "", span(47, 61)...)
	g.fires("system-007", 61)
	g.failEndpoint("S1", "RAFTZ", "missing", "", 46)
	g.fires("system-007", 61, "")
	if r := g.eval("system-007", 61, map[string]string{"warn_share": "0.3"}); len(r) != 0 {
		t.Fatalf("16 of 61 with warn_share 0.3: %s", rowsText(r))
	}
}

// A server that answers nothing is server-001's: its collections are
// partial, but not incomplete here.
func TestCollectionsIncompleteLeavesMissingServersOut(t *testing.T) {
	f := newFx(t)
	f.epochs(1, 61, three...)
	f.exec(`UPDATE epochs SET partial = true, missing_names = ['nats-2'], observed_names = ['nats-0', 'nats-1'] WHERE epoch > 20`)
	f.fires("system-007", 61)
}

// Failures of nats-lens itself count, with their latest error.
func TestCollectionsIncompleteCollectorErrors(t *testing.T) {
	f := newFx(t)
	f.cluster(61)
	f.exec(`UPDATE epochs SET partial = true, errors = ['publish STATSZ: nats: connection closed'] WHERE epoch BETWEEN 30 AND 49`)
	r := f.fires("system-007", 61, "")
	want(t, r[""], "warning", "20 of 61 collections", "nats-lens failed itself in 20 (publish STATSZ: nats: connection closed)")
}

// A short history, or a gap that leaves fewer than min_collections (10)
// collections in the lookback, is not judged.
func TestCollectionsIncompleteNeedsCollections(t *testing.T) {
	f := newFx(t)
	f.cluster(9)
	f.failEndpoint("S1", "RAFTZ", "missing", "", span(1, 9)...)
	f.fires("system-007", 9)
	f.epochAt(10, at(9).Add(2*time.Hour), three)
	f.failEndpoint("S1", "RAFTZ", "missing", "", 10)
	f.fires("system-007", 10)
	g := newFx(t)
	g.cluster(10)
	g.failEndpoint("S1", "RAFTZ", "missing", "", span(1, 10)...)
	g.fires("system-007", 10, "")
}

// A restarting server answers late: its failures within
// checks.restart_window of its start do not count.
func TestCollectionsIncompleteDuringRestart(t *testing.T) {
	f := newFx(t)
	f.metaCluster(61, nil)
	f.restartAt(40)
	f.failEndpoint("S1b", "RAFTZ", "missing", "", span(40, 59)...)
	f.fires("system-007", 61)
}

// A firing finding stays while the share is at or above resolve_ratio
// (0.5) of warn_share, 12.5%, and resolves below it.
func TestCollectionsIncompleteResolves(t *testing.T) {
	f := newFx(t)
	f.cluster(61)
	f.failEndpoint("S1", "RAFTZ", "missing", "", span(1, 8)...)
	f.fires("system-007", 61)
	f.firingSystem("system-007", 60)
	r := f.fires("system-007", 61, "")
	want(t, r[""], "warning", "8 of 61 collections")
	g := newFx(t)
	g.cluster(61)
	g.failEndpoint("S1", "RAFTZ", "missing", "", span(1, 7)...)
	g.firingSystem("system-007", 60)
	g.fires("system-007", 61)
}

// The store's notes in a collection's errors, such as a value a newer
// server sent, are no failure: a newer server noted in every collection
// does not make the collections incomplete.
func TestCollectionsIncompleteLeavesStoreNotesOut(t *testing.T) {
	f := newFx(t)
	f.cluster(61)
	f.exec(`UPDATE epochs SET errors = [?]`, store.NotePrefix+`nats-0 sent stream config.retention "foo", which this nats-server version does not know, for 1 stream; stored as "foo"`)
	f.fires("system-007", 61)
	f.exec(`UPDATE epochs SET partial = true, errors = list_append(errors, 'publish JSZ: nats: timeout') WHERE epoch > 40`)
	r := f.fires("system-007", 61, "")
	want(t, r[""], "warning", "nats-lens failed itself in 21 (publish JSZ: nats: timeout)")
}

// A server that announced lame duck or shutdown leaves requests
// unanswered on its way out; those do not count, so a rolling restart of
// many servers does not add up.
func TestCollectionsIncompleteLeavesAnnouncedServersOut(t *testing.T) {
	f := newFx(t)
	f.cluster(61)
	f.event(1, at(30).Add(-10*time.Second), "server.lameduck", m{"source": "sys", "server_name": "nats-1"})
	f.failEndpoint("S1", "RAFTZ", "missing", "", span(30, 49)...)
	f.fires("system-007", 61)
	f.exec(`DELETE FROM events`)
	f.fires("system-007", 61, "")
}

// An empty account RAFTZ reply is still ok. The check reads request
// outcomes, not the presence of raft groups or streams for that account.
// A second account's replies do not stand in for a lost APP reply.
func TestCollectionsIncompleteAccountRaftzBoundary(t *testing.T) {
	f := newFx(t)
	f.cluster(61)
	for e := int64(1); e <= 61; e++ {
		for _, account := range []string{"APP", "SHIPPING"} {
			f.row("epoch_endpoints", m{"epoch": e, "server_id": "S1", "endpoint": "RAFTZ:" + account, "state": "ok"})
		}
	}
	f.fires("system-007", 61)
	f.exec(`UPDATE epoch_endpoints SET state = 'missing' WHERE server_id = 'S1' AND endpoint = 'RAFTZ:APP' AND epoch >= 46`)
	f.exec(`UPDATE epochs SET partial = true, endpoint_failures = 1 WHERE epoch >= 46`)
	r := f.fires("system-007", 61, "")
	want(t, r[""], "warning", "16 of 61 collections", "RAFTZ:APP from nats-1 unanswered in 16")
}

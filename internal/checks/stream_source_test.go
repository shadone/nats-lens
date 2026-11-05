package checks

import "testing"

func TestSourceLagging(t *testing.T) {
	f := newFx(t)
	f.cluster(11)
	for _, s := range []string{"search-index", "catching-up", "fine", "keeps"} {
		f.streamConfig("S0", s, 1, 11, nil)
	}
	f.exec(`UPDATE stream_config SET sources = ['catalog-changes', 'price-changes'] WHERE stream <> 'fine'`)
	f.exec(`UPDATE stream_config SET sources = ['catalog-changes'] WHERE stream = 'fine'`)
	src := func(e int64, s string, lag int64, n int) {
		f.streamStats(e, "S0", s, m{"leader": "nats-0", "is_leader": true, "sources_count": n, "sources_max_lag": lag})
	}
	for e := int64(1); e <= 11; e++ {
		src(e, "search-index", 30000+e*100, 2)
		src(e, "catching-up", 300000-e*20000, 2)
		src(e, "fine", 10000, 1)
		src(e, "keeps", 8500, 2)
		// A mirror has no sources; its lag is stream-015's.
		f.streamMirrorAt(e, "order-archive", 90000+e, 0, nil)
	}
	f.streamFiringAt("stream-016", "APP/keeps", 10)
	r := f.fires("stream-016", 11, "APP/search-index", "APP/keeps")
	want(t, r["APP/search-index"], "warning",
		"Leader nats-0: the most lagging of its 2 sources (catalog-changes, price-changes) is 31100 messages behind, not catching up over the last 5m (30100 at its start).")
	want(t, r["APP/keeps"], "warning", "is 8500 messages behind.")
	if r := f.eval("stream-016", 11, map[string]string{"resolve_ratio": "0.9"}); len(r) != 1 {
		t.Fatalf("resolve_ratio 0.9: %s", rowsText(r))
	}
}

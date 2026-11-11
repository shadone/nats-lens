package storeprov_test

import (
	"testing"
	"time"
)

// A client pod row whose namespace watch failed two days ago
// stays live (ended_at NULL, last_seen two days old). A connection now
// from that address must not be matched to it.
func TestStaleLiveClientPodRowMatchesNothing(t *testing.T) {
	p := appsProvider(t, appsLast)
	seen := appsLast.Add(-48 * time.Hour).UnixMicro()
	if _, err := p.St.DB().ExecContext(ctx, `INSERT INTO client_pods (uid, ip, namespace, pod, node, workload_kind, workload, app_name,
  host_network, first_seen, last_seen, ended_at) VALUES ('uid-gone', '10.1.0.7', 'billing', 'gone-pod', 'node-9', 'Deployment', 'gone',
  '', false, make_timestamp(?::BIGINT), make_timestamp(?::BIGINT), NULL)`, appsLast.Add(-72*time.Hour).UnixMicro(), seen); err != nil {
		t.Fatal(err)
	}
	res := appsPage(t, appsClient(t, p), appsLast.Add(time.Minute), "", false)
	cd := appNamed(res, "callback-dispatcher").GetRunsOn()
	if cd.GetMatched() != 1 {
		t.Errorf("callback-dispatcher matched %d connections, want 1: 10.1.0.7 matched to a pod last seen 48 hours ago (%v)", cd.GetMatched(), cd)
	}
}

// The freshness boundary: a live row seen 14 minutes before the epoch
// matches, one seen 16 minutes before does not; an ended row matches
// through its end, however old its last sighting.
func TestClientPodFreshnessBoundary(t *testing.T) {
	for _, c := range []struct {
		name      string
		seen      time.Duration
		ended     *time.Duration
		wantMatch int32
	}{
		{"live, seen 14 minutes before", 14 * time.Minute, nil, 2},
		{"live, seen 16 minutes before", 16 * time.Minute, nil, 1},
		{"ended after the epoch, seen 48 hours before", 48 * time.Hour, ptrDur(-5 * time.Minute), 2},
		{"ended before the epoch", 48 * time.Hour, ptrDur(time.Minute), 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := appsProvider(t, appsLast)
			var ended any
			if c.ended != nil {
				ended = appsLast.Add(-*c.ended).UnixMicro()
			}
			if _, err := p.St.DB().ExecContext(ctx, `INSERT INTO client_pods (uid, ip, namespace, pod, node, workload_kind, workload, app_name,
  host_network, first_seen, last_seen, ended_at) VALUES ('uid-b', '10.1.0.7', 'shop', 'cd-b', 'node-2', 'StatefulSet', 'callback-dispatcher',
  '', false, make_timestamp(?::BIGINT), make_timestamp(?::BIGINT), make_timestamp(?::BIGINT))`,
				appsLast.Add(-72*time.Hour).UnixMicro(), appsLast.Add(-c.seen).UnixMicro(), ended); err != nil {
				t.Fatal(err)
			}
			res := appsPage(t, appsClient(t, p), appsLast.Add(time.Minute), "", false)
			if got := appNamed(res, "callback-dispatcher").GetRunsOn().GetMatched(); got != c.wantMatch {
				t.Errorf("matched %d, want %d", got, c.wantMatch)
			}
		})
	}
}

func ptrDur(d time.Duration) *time.Duration { return &d }

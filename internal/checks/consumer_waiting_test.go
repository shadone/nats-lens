package checks

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/config"
)

// consumer-015: a pull consumer's waiting requests at max_waiting in two
// collections in a row fire; one collection, a push consumer, a paused one,
// a follower's row and a consumer recreated in between do not.
func TestPullRequestsAtMaxWaiting(t *testing.T) {
	f := newFx(t)
	f.cluster(3)
	newer := t0.Add(-time.Minute)
	for _, c := range []struct {
		name    string
		waiting [3]int64
		cols    m
		stats   m
	}{
		{"full", [3]int64{512, 512, 512}, nil, nil},
		{"once", [3]int64{10, 10, 512}, nil, nil},
		{"near", [3]int64{511, 511, 511}, nil, nil},
		{"push", [3]int64{512, 512, 512}, m{"deliver_subject": "deliver.push"}, nil},
		{"paused", [3]int64{512, 512, 512}, nil, m{"paused": true}},
	} {
		f.consumerConfig("S0", "jobs", c.name, 1, 3, c.cols)
		for e := int64(1); e <= 3; e++ {
			f.consumerStats(e, "S0", "jobs", c.name, merge(m{"is_leader": true, "num_waiting": c.waiting[e-1], "num_pending": 7}, c.stats))
			// A follower holds no pull requests; its row has none.
			f.consumerStats(e, "S1", "jobs", c.name, m{"is_leader": false})
		}
	}
	// recreated: full in epoch 2 as an older consumer, then created again.
	f.consumerConfig("S0", "jobs", "recreated", 1, 3, nil)
	f.consumerStats(2, "S0", "jobs", "recreated", m{"is_leader": true, "num_waiting": 512})
	f.consumerStats(3, "S0", "jobs", "recreated", m{"is_leader": true, "num_waiting": 512, "created": newer})

	f.fires("consumer-015", 1)
	r := f.fires("consumer-015", 3, "APP/jobs/full")
	want(t, r["APP/jobs/full"], "warning", "512 pull requests waiting of max_waiting 512: new fetches are refused; 7 messages waiting")
	if v := r["APP/jobs/full"].Value; v == nil || *v != 1 {
		t.Errorf("value = %v, want 1", v)
	}
	if r["APP/jobs/full"].RelatedConfig["max_waiting"] != "512" || r["APP/jobs/full"].RelatedSource != "nats-0" {
		t.Errorf("related = %v %q", r["APP/jobs/full"].RelatedConfig, r["APP/jobs/full"].RelatedSource)
	}
	// With warn_ratio lowered, 511 of 512 fires as close to the limit.
	r = f.eval("consumer-015", 3, map[string]string{"warn_ratio": "0.9"})
	want(t, r["APP/jobs/near"], "warning", "511 pull requests waiting of max_waiting 512, close to the limit")
}

// A firing finding stays while the waiting requests are at or above
// resolve_ratio (0.9) of max_waiting, 461 of 512, and resolves below; a
// consumer that was not firing needs the limit for min_epochs collections.
func TestPullRequestsAtMaxWaitingHysteresis(t *testing.T) {
	f := newFx(t)
	f.cluster(3)
	firing := func(key string) {
		f.row("findings", m{"id": "consumer-015:" + key, "check_code": "consumer-015", "entity_type": "consumer", "entity_key": key,
			"severity": "warning", "state": "firing", "last_transition": "firing", "since": at(1), "since_epoch": 1,
			"transitioned_at": at(1), "last_seen_epoch": 2, "updated_epoch": 2, "detail": "x"})
	}
	for name, waiting := range map[string]int64{"holding": 461, "dropped": 460, "new": 470} {
		f.consumerConfig("S0", "jobs", name, 1, 3, nil)
		f.consumerStats(2, "S0", "jobs", name, m{"is_leader": true, "num_waiting": 512, "num_pending": 7})
		f.consumerStats(3, "S0", "jobs", name, m{"is_leader": true, "num_waiting": waiting, "num_pending": 7})
	}
	firing("APP/jobs/holding")
	firing("APP/jobs/dropped")
	r := f.fires("consumer-015", 3, "APP/jobs/holding")
	want(t, r["APP/jobs/holding"], "warning", "461 pull requests waiting of max_waiting 512, close to the limit")
}

// A consumer without a leader in the collection is not observed: no
// finding from the followers' rows.
func TestPullRequestsAtMaxWaitingWithoutLeader(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	f.consumerConfig("S0", "jobs", "worker", 1, 2, nil)
	f.consumerConfig("S1", "jobs", "worker", 1, 2, nil)
	f.consumerStats(1, "S0", "jobs", "worker", m{"is_leader": true, "num_waiting": 512})
	f.consumerStats(2, "S0", "jobs", "worker", m{"is_leader": false, "leader": ""})
	f.consumerStats(2, "S1", "jobs", "worker", m{"is_leader": false, "leader": ""})
	f.fires("consumer-015", 2)
}

// A full queue with nothing waiting is idle workers holding their fetches,
// and a max_waiting of 1 is full with one worker's fetch: neither is a
// finding. With messages waiting, two of two fires.
func TestPullRequestsAtMaxWaitingByDesign(t *testing.T) {
	f := newFx(t)
	f.cluster(3)
	for _, c := range []struct {
		name             string
		max, wait, ready int64
	}{{"exclusive", 1, 1, 5}, {"idle", 2, 2, 0}, {"pair", 2, 2, 5}} {
		f.consumerConfig("S0", "jobs", c.name, 1, 3, m{"max_waiting": c.max})
		for e := int64(1); e <= 3; e++ {
			f.consumerStats(e, "S0", "jobs", c.name, m{"is_leader": true, "num_waiting": c.wait, "num_pending": c.ready})
		}
	}
	r := f.fires("consumer-015", 3, "APP/jobs/pair")
	want(t, r["APP/jobs/pair"], "warning", "2 pull requests waiting of max_waiting 2: new fetches are refused; 5 messages waiting.")
	r = f.eval("consumer-015", 3, map[string]string{"min_max_waiting": "1"})
	want(t, r["APP/jobs/exclusive"], "warning", "1 pull request waiting of max_waiting 1: new fetches are refused")
	if _, ok := r["APP/jobs/idle"]; ok {
		t.Errorf("idle fired: %s", r["APP/jobs/idle"].Detail)
	}
}

// Run through the engine collection by collection, so that each
// collection sees the finding the previous one left: a busy consumer at
// the limit whose backlog touches 0 for one collection keeps its finding;
// one with no message waiting for min_epochs (2) collections resolves.
func TestPullRequestsAtMaxWaitingEmptyBacklog(t *testing.T) {
	f := newFx(t)
	f.cluster(6)
	for name, pend := range map[string][]int64{"busy": {7, 7, 0, 7, 7, 7}, "drained": {7, 7, 0, 0, 7, 7}} {
		f.consumerConfig("S0", "jobs", name, 1, 6, nil)
		for e := int64(1); e <= 6; e++ {
			f.consumerStats(e, "S0", "jobs", name, m{"is_leader": true, "num_waiting": 512, "num_pending": pend[e-1]})
		}
	}
	set, err := Load(config.Checks{Timeout: config.Duration(10 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Set: set, only: map[string]bool{"consumer-015": true}}
	got := map[string][]string{}
	for ep := int64(1); ep <= 6; ep++ {
		if _, err := e.Run(context.Background(), f.st, ep); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"busy", "drained"} {
			var state string
			err := f.st.DB().QueryRow(`SELECT coalesce(max(CASE WHEN state = 'firing' AND last_seen_epoch = ? THEN 'F' END), '-')
  FROM findings WHERE check_code = 'consumer-015' AND entity_key = ?`, ep, "APP/jobs/"+name).Scan(&state)
			if err != nil {
				t.Fatal(err)
			}
			got[name] = append(got[name], state)
		}
	}
	for name, want := range map[string]string{"busy": "-FFFFF", "drained": "-FF--F"} {
		if g := strings.Join(got[name], ""); g != want {
			t.Errorf("%s firing by collection %s, want %s", name, g, want)
		}
	}
}

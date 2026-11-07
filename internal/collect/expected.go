package collect

import (
	"sort"
	"strings"
	"time"
)

// Tracker remembers when each server name last answered, so that a server
// that stops answering is still expected for a while. Server IDs change on
// every restart, so servers are tracked by name.
type Tracker struct {
	forget   time.Duration
	lastSeen map[string]time.Time
}

// NewTracker returns a tracker that forgets a server after it has not
// answered for the given duration.
func NewTracker(forget time.Duration) *Tracker {
	return &Tracker{forget: forget, lastSeen: map[string]time.Time{}}
}

// Seed records a server seen at a time, for example from the database at start.
func (t *Tracker) Seed(name string, at time.Time) {
	if name == "" {
		return
	}
	if at.After(t.lastSeen[name]) {
		t.lastSeen[name] = at
	}
}

// Known returns the names seen within the forget window before now, sorted.
func (t *Tracker) Known(now time.Time) []string {
	var out []string
	for n, at := range t.lastSeen {
		if now.Sub(at) <= t.forget {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// observe marks every server in the snapshot as seen at its start time.
func (t *Tracker) observe(s *Snapshot) {
	for _, v := range s.Servers {
		t.Seed(v.Info.Name, s.StartedAt)
	}
}

// peerNames returns server names that answering servers report as their
// cluster peers: route remotes and JetStream meta group members.
func peerNames(s *Snapshot) []string {
	var out []string
	for _, v := range s.Servers {
		if v.Routez != nil {
			for _, r := range v.Routez.Routes {
				if r != nil {
					out = append(out, r.RemoteName)
				}
			}
		}
		if v.Jsz != nil && v.Jsz.Meta != nil {
			out = append(out, v.Jsz.Meta.Leader)
			for _, p := range v.Jsz.Meta.Replicas {
				if p != nil {
					out = append(out, p.Name)
				}
			}
		}
	}
	return out
}

// expectedNames is the union of the tracker's known names, the servers
// that answered, and the peers they report. Empty names are dropped.
func expectedNames(known []string, s *Snapshot) []string {
	set := map[string]bool{}
	add := func(n string) {
		if n = strings.TrimSpace(n); n != "" {
			set[n] = true
		}
	}
	for _, n := range known {
		add(n)
	}
	for _, n := range s.Observed() {
		add(n)
	}
	for _, n := range peerNames(s) {
		add(n)
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

package storeprov

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// podCacheSize is how many ranges the pod name cache keeps: the overview,
// a detail page and a few zoomed ranges in use at once.
const podCacheSize = 8

type podRange struct{ start, end time.Time }

type podEntry struct {
	names []string
	used  int
}

// podNameCache keeps the pod names of the connections of the ranges
// asked for recently, so that the Kubernetes lane does not scan the
// connections on every request.
type podNameCache struct {
	mu      sync.Mutex
	entries map[podRange]*podEntry
	clock   int
	// reads counts the scans, for tests.
	reads int
}

// clientPodNames returns the pod names that follow "@" in the names of the
// connections seen from an hour before start until end (services that
// name a connection service@pod do). An answer is kept per range, with start
// and end truncated to the collect interval, so a live range that moves
// with the clock reads the connections once per epoch; the least recently
// used of more than podCacheSize ranges is dropped.
func (p *Provider) clientPodNames(ctx context.Context, start, end time.Time) ([]string, error) {
	step := max(p.Interval, time.Second)
	key := podRange{start.Truncate(step), end.Truncate(step)}
	c := &p.podCache
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clock++
	if e, ok := c.entries[key]; ok {
		e.used = c.clock
		return e.names, nil
	}
	rows, err := p.db().QueryContext(ctx, `
SELECT DISTINCT split_part(c.name, '@', 2) FROM connections c
WHERE c.name LIKE '%@%' AND c.epoch IN (SELECT epoch FROM epochs WHERE started_at >= ? AND started_at < ?)`,
		start.Add(-time.Hour), end)
	if err != nil {
		return nil, fmt.Errorf("client pod names: %w", err)
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if c.entries == nil {
		c.entries = map[podRange]*podEntry{}
	}
	if len(c.entries) >= podCacheSize {
		var oldest podRange
		least := -1
		for k, e := range c.entries {
			if least < 0 || e.used < least {
				oldest, least = k, e.used
			}
		}
		delete(c.entries, oldest)
	}
	c.entries[key] = &podEntry{names: names, used: c.clock}
	c.reads++
	return names, nil
}

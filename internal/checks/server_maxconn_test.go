package checks

import (
	"testing"
	"time"
)

// Connections at 80% of max_connections warn, at 95% are critical; one
// below the level does not fire.
func TestConnectionsNearMax(t *testing.T) {
	f := newFx(t)
	f.cluster(1)
	f.stats(1, "S0", m{"connections": 800, "max_connections": 1000})
	f.stats(1, "S1", m{"connections": 950, "max_connections": 1000})
	f.stats(1, "S2", m{"connections": 799, "max_connections": 1000})
	r := f.fires("server-020", 1, "nats-0", "nats-1")
	want(t, r["nats-0"], "warning", "nats-0 holds 800 client connections of max_connections 1000 (80%).")
	want(t, r["nats-1"], "critical", "(95%)")
}

// A collection whose VARZ failed (connections from STATSZ, no limit)
// keeps the process's newest limit; a server without a limit, or one that
// did not answer, is not judged.
func TestConnectionsNearMaxSources(t *testing.T) {
	f := newFx(t)
	f.cluster(2)
	f.stats(1, "S0", m{"connections": 900, "max_connections": 1000})
	f.stats(2, "S0", m{"connections": 900})
	f.stats(2, "S1", m{"connections": 900})
	f.stats(2, "S2", m{"connections": 900, "max_connections": 0})
	f.fires("server-020", 2, "nats-0")
	// nats-0 restarts at epoch 3: the new process has no limit of its own
	// yet and is not judged by the old one's.
	f.epochs(3, 3, three...)
	f.server("N0", "nats-0", 3, 3, at(3).Add(-time.Second), "2.15.0")
	f.stats(3, "N0", m{"connections": 900})
	f.fires("server-020", 3)
}

// A firing finding stays down to 90% of the warning level and resolves
// below it.
func TestConnectionsNearMaxResolve(t *testing.T) {
	f := newFx(t)
	f.cluster(3)
	f.stats(2, "S0", m{"connections": 721, "max_connections": 1000})
	f.stats(3, "S0", m{"connections": 719, "max_connections": 1000})
	f.fires("server-020", 2)
	f.row("findings", m{"id": "server-020:nats-0", "check_code": "server-020", "entity_type": "server", "entity_key": "nats-0",
		"severity": "warning", "state": "firing", "last_transition": "firing", "since": at(1), "since_epoch": 1,
		"transitioned_at": at(1), "last_seen_epoch": 1, "updated_epoch": 1, "detail": "x"})
	f.fires("server-020", 2, "nats-0")
	f.exec(`UPDATE findings SET last_seen_epoch = 2`)
	f.fires("server-020", 3)
}

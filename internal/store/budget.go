package store

import "github.com/shadone/nats-lens/internal/admit"

// backgroundConns are the connections the store keeps for work outside the
// query budget: the epoch write, the checks, the archive export, event,
// notification, Kubernetes and retention writes, and API writes, which run
// in transactions. Proposed, not yet confirmed.
const backgroundConns = 8

// SetQueryBudget bounds the queries that API calls and /metrics run on
// the store at once to n, at least one; the others wait in turn, up to
// their deadline (see package admit). Epoch writes, checks and every
// statement in a transaction are not counted. It also keeps up to n plus
// the background work's connections open between queries, rather than
// database/sql's default of two, so that a burst does not open and close
// a DuckDB connection per query. Call it before the store serves
// queries.
func (s *Store) SetQueryBudget(n int) {
	if s.guard == nil {
		return
	}
	g := admit.New(n, s.guard.systemLabel)
	s.guard.gate.Store(g)
	s.db.SetMaxIdleConns(g.Limit() + backgroundConns)
}

// QueryBudget returns the gate SetQueryBudget set, or nil.
func (s *Store) QueryBudget() *admit.Gate {
	if s.guard == nil {
		return nil
	}
	return s.guard.gate.Load()
}

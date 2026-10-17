package checks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// rowQuerier is a database or a connection.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// The loopback guard: loopback clients count as tooling unless they are
// most of the clients, which a sidecar that makes every inbound connection
// look local would cause.
const (
	// loopbackWindow is how many epochs the loopback share is taken over.
	loopbackWindow = 10
	// loopbackOffAbove and loopbackOnBelow are the hysteresis bounds:
	// the rule stops above the first share and returns below the second.
	loopbackOffAbove = 0.6
	loopbackOnBelow  = 0.4
	// loopbackMinOthers is the fewest non-own connections an epoch needs
	// for the guard to change its decision; a lone loopback CLI keeps the
	// previous one (tooling unless the guard is off).
	loopbackMinOthers = 3
)

// nextLoopback is the guard's decision for an epoch from the previous
// epoch's decision, the loopback and non-own connections of the trailing
// loopbackWindow epochs, and the epoch's own non-own connections.
func nextLoopback(prevOn bool, loop, others, epochOthers int64) bool {
	if epochOthers < loopbackMinOthers || others == 0 {
		// Too few connections to judge: the decision stands, so a thin
		// epoch neither resets nor trips the guard.
		return prevOn
	}
	share := float64(loop) / float64(others)
	switch {
	case prevOn && share > loopbackOffAbove:
		return false
	case !prevOn && share < loopbackOnBelow:
		return true
	}
	return prevOn
}

// StoredLoopback reads the guard's stored decision for epoch; an epoch
// without one (older data, or no epoch) reads as on.
func StoredLoopback(ctx context.Context, q rowQuerier, epoch int64) (bool, error) {
	var on sql.NullBool
	err := q.QueryRowContext(ctx, `SELECT loopback_tooling FROM epochs WHERE epoch = ?`, epoch).Scan(&on)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !on.Valid) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("loopback decision: %w", err)
	}
	return on.Bool, nil
}

// LoopbackDecision returns the guard's decision for epoch: the stored one
// when the epoch has one, else the one that follows from the latest
// decision stored in the trailing window and the trailing share. The engine stores it
// after the epoch's checks, so an epoch is judged the same whenever it is
// asked about. stored reports that it was read, not computed.
func LoopbackDecision(ctx context.Context, q rowQuerier, epoch int64, names []string) (on, stored bool, err error) {
	// The previous decision is the latest one stored among the trailing
	// window's earlier epochs, so an epoch whose checks failed or were
	// skipped (no decision) does not reset the guard; with none, on.
	var cur, prev sql.NullBool
	err = q.QueryRowContext(ctx, `SELECT loopback_tooling,
  (SELECT arg_max(p.loopback_tooling, p.epoch) FILTER (WHERE p.loopback_tooling IS NOT NULL)
   FROM (SELECT epoch, loopback_tooling FROM epochs WHERE epoch < ? ORDER BY epoch DESC LIMIT ?) p)
FROM epochs WHERE epoch = ?`, epoch, loopbackWindow, epoch).Scan(&cur, &prev)
	if errors.Is(err, sql.ErrNoRows) {
		return true, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("loopback decision: %w", err)
	}
	if cur.Valid {
		return cur.Bool, true, nil
	}
	prevOn := !prev.Valid || prev.Bool
	var loop, others, epochOthers int64
	err = q.QueryRowContext(ctx, `
WITH w AS (SELECT epoch FROM epochs WHERE epoch <= ? ORDER BY epoch DESC LIMIT ?)
SELECT count(*) FILTER (WHERE coalesce(c.ip, '') IN ('127.0.0.1', '::1')), count(*),
  count(*) FILTER (WHERE c.epoch = ?)
FROM connections c WHERE c.epoch IN (SELECT epoch FROM w)
  AND NOT list_contains(`+listLiteral(names)+`, coalesce(c.name, ''))`, epoch, loopbackWindow, epoch).Scan(&loop, &others, &epochOthers)
	if err != nil {
		return false, false, fmt.Errorf("loopback connections: %w", err)
	}
	return nextLoopback(prevOn, loop, others, epochOthers), false, nil
}

// LoopbackIsTooling is the checks.loopback_is_tooling setting.
func (s *Set) LoopbackIsTooling() bool { return s.loopbackTooling }

// ToolingNames are nats-lens's own connection names.
func (s *Set) ToolingNames() []string { return append([]string(nil), s.tooling...) }

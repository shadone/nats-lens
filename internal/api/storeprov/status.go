package storeprov

import (
	"cmp"
	"context"
	"slices"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// ranFor reports whether every check of the entity types ran ok in an
// epoch; a disabled check counts as ran, being a decision and not a gap.
type ranFor func(types ...string) bool

// checksRan reads the epoch's check runs. Without a check set, a type
// counts as ran when some check ran ok and none failed. An entity without
// a firing finding is ok only when the checks of its type ran, and has no
// data otherwise.
func (p *Provider) checksRan(ctx context.Context, epoch int64) (ranFor, error) {
	rows, err := p.db().QueryContext(ctx, `SELECT check_code, status FROM check_runs WHERE epoch = ?`, epoch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	status := map[string]string{}
	for rows.Next() {
		var code, st string
		if err := rows.Scan(&code, &st); err != nil {
			return nil, err
		}
		status[code] = st
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return func(types ...string) bool {
		if p.Checks == nil {
			ok := false
			for _, st := range status {
				if st == "error" {
					return false
				}
				ok = ok || st == "ok"
			}
			return ok
		}
		for _, c := range p.Checks.Checks() {
			if !slices.Contains(types, c.Entity) {
				continue
			}
			if st := status[c.Code]; st != "ok" && st != "disabled" {
				return false
			}
		}
		return len(status) > 0
	}, nil
}

// orNoData turns ok into no data when the checks did not run.
func orNoData(st v1.Status, ran bool) v1.Status {
	if st == v1.Status_STATUS_OK && !ran {
		return v1.Status_STATUS_NO_DATA
	}
	return st
}

// firingFindings reads the findings firing now among those of the range,
// worst first.
func (p *Provider) firingFindings(ctx context.Context, r *v1.TimeRange) ([]*v1.Finding, error) {
	fs, err := p.findings(ctx, r)
	if err != nil {
		return nil, err
	}
	return firing(fs), nil
}

// findingsFor keeps the findings of an entity, a stream's including its
// consumers'.
func findingsFor(fs []*v1.Finding, e *v1.EntityRef) []*v1.Finding {
	var out []*v1.Finding
	for _, f := range fs {
		if matchesEntity(f.GetEntity(), e) {
			out = append(out, f)
		}
	}
	return out
}

// applyStreamStatus sets each stream's status and finding codes from the
// firing findings, and the consumers the inventory expects.
func applyStreamStatus(ss []*v1.StreamSummary, cur []*v1.Finding, ran ranFor, expected map[string]int32, named map[string]bool) {
	for _, s := range ss {
		s.Status, s.FindingCodes = statusOf(cur, s.GetEntity())
		s.Status = orNoData(s.Status, ran("stream"))
		if named[s.GetEntity().GetKey()] {
			s.ConsumersExpected = ptr(expected[s.GetEntity().GetKey()])
		}
	}
	slices.SortStableFunc(ss, func(a, b *v1.StreamSummary) int {
		return cmp.Or(cmp.Compare(statusRank(a.GetStatus()), statusRank(b.GetStatus())), cmp.Compare(a.GetEntity().GetKey(), b.GetEntity().GetKey()))
	})
}

// applyConsumerStatus sets each consumer's status and finding codes from
// the firing findings; a missing consumer without a finding has no data.
func applyConsumerStatus(cs []*v1.ConsumerSummary, cur []*v1.Finding, ran ranFor) {
	for _, c := range cs {
		st, codes := statusOf(cur, c.GetEntity())
		st = orNoData(st, ran("consumer"))
		if !c.GetPresent() && len(codes) == 0 {
			st = v1.Status_STATUS_NO_DATA
		}
		c.Status, c.FindingCodes = st, codes
	}
	slices.SortStableFunc(cs, func(a, b *v1.ConsumerSummary) int {
		return cmp.Or(cmp.Compare(statusRank(a.GetStatus()), statusRank(b.GetStatus())), cmp.Compare(a.GetEntity().GetKey(), b.GetEntity().GetKey()))
	})
}

// applyServerStatus sets each server's status from the firing findings on
// it; a server without one is ok when it answered in the newest epoch and
// the checks ran, and has no data otherwise.
func applyServerStatus(ss []*v1.ServerSummary, cur []*v1.Finding, ran ranFor) {
	for _, s := range ss {
		st, codes := statusOf(cur, &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: s.GetName()})
		st = orNoData(st, ran("server"))
		if len(codes) == 0 && !s.GetPresent() {
			st = v1.Status_STATUS_NO_DATA
		}
		s.Status = st
	}
}

package storeprov

import (
	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// routineSQL is true for a routine event e, whose epoch (the newest that
// started at or before the event) is joined as x: a routine kind; an API
// audit record of a successful call that changes nothing; or a connect or
// disconnect of a tooling client. Tooling is the rule of the client
// checks: one of nats-lens's own connection names, or a loopback address
// while the epoch's stored loopback decision is on (an epoch without one,
// or no epoch, counts as on) and loopback_is_tooling is set.
// checks.include_tooling does not apply: hiding routine events only
// changes what is shown.
func (p *Provider) routineSQL() (string, []any) {
	return `(list_contains(?::VARCHAR[], e.kind)
  OR (e.kind = ? AND coalesce(e.reason, '') = ''
    AND NOT regexp_matches(coalesce(json_extract_string(e.raw, '$.subject'), ''), ?))
  OR (list_contains(?::VARCHAR[], e.kind)
    AND (list_contains(?::VARCHAR[], coalesce(e.client_name, ''))
      OR (? AND coalesce(e.client_host, '') IN ('127.0.0.1', '::1') AND coalesce(x.loopback_tooling, true)))))`,
		[]any{api.RoutineKinds, api.AuditKind, api.MutatingAuditPattern, api.ToolingKinds, append([]string{}, p.ToolingNames...),
			!p.LoopbackNotTooling}
}

// richEvents is the CTE b: the events that match req, with the column
// routine. upto, when set, leaves out events with a higher ID.
func (p *Provider) richEvents(req *v1.ListEventsRequest, upto int64) (string, []any) {
	where, args := eventFilter(req)
	if upto > 0 {
		where, args = where+" AND e.id <= ?", append(args, upto)
	}
	rt, rargs := p.routineSQL()
	return `WITH b AS (SELECT e.*, ` + rt + ` AS routine
FROM events e ASOF LEFT JOIN epochs x ON e.time >= x.started_at WHERE ` + where + `)
`, append(rargs, args...)
}

// histogramClass sorts an event into a histogram column: routine first,
// then warning for a severity of warning or more, else notable.
func histogramClass(b *v1.EventBucket, routine bool, sev v1.Severity, n int32) {
	switch {
	case routine:
		b.Routine += n
	case sev >= v1.Severity_SEVERITY_WARNING:
		b.Warning += n
	default:
		b.Notable += n
	}
}

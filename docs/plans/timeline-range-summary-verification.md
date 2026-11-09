# Timeline selected-range summary correction

The native timeline query selects its newest evaluation within the requested range; the demo derives its summary from selected cells. The legacy `now` field therefore does not independently read current check state. UI labels now say “Range summary”, and mark previews explain selected-range evidence and current mute rules. API behavior is unchanged. Latest linked finding evidence remains separately labeled in occurrence details. This presentation policy is proposed, not yet confirmed.

Two focused UI files: initial runtime RED **3 failed / 10 passed**, fixed **13 passed**. Removing StatusTimeline.tsx changes reproduces the same three failures; restoration passes. TypeScript passes. Logs: `/private/tmp/nats-range-summary-{red,green,removed,restored}.log`. No backend algorithm, schema, dependency, full-suite or browser claim.

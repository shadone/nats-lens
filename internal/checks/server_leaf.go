package checks

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/leafhealth"
	"github.com/shadone/nats-lens/internal/store"
)

const LeafHealthCode = "server-026"

const leafUnknownSuffix = " Current leaf observation coverage is uncertain; retaining earlier evidence until collection coverage establishes recovery."

func leafHealthCheck() *Check {
	return &Check{Code: LeafHealthCode, Name: "Leaf node disconnects or repeated reconnects", Entity: "system", Category: "health", Severity: "warning", Kind: "operational", builtin: true,
		Description: "Observed leaf sites absent for two consecutive fully observed collections, or with at least three observed connection drops in one hour. Hub handoffs are excluded. A system finding names sites and accounts without blaming a hub. Missing LEAFZ, restarts and gaps break evidence; unobserved expected sites cannot fire.",
		Remediation: "Read the named sites on Topology. Compare their network paths and remote process logs with the hub logs. Confirm whether the absence or reconnects are expected before changing configuration. NATS provides no leaf disconnect event; these are collection observations.",
		ValueName:   "affected leaf sites", ValueBasis: "site name (or IP fallback) and account pairs meeting either observed leaf health condition"}
}

// leafHealth uses one bounded SQL batch over 24 hours, plus one previous-finding
// read on uncertainty, independent of the site count. Only per-site evidence
// leaves DuckDB. All timestamps are compared directly to columns (host TZ safe).
func (e *Engine) leafHealth(ctx context.Context, conn *sql.Conn, epoch int64, at time.Time) ([]store.FindingRow, error) {
	if e.Interval <= 0 {
		return carryLeafHealth(ctx, conn, epoch)
	}
	rows, err := conn.QueryContext(ctx, `
WITH ep AS (SELECT epoch, started_at FROM epochs WHERE started_at>=? AND epoch<=?),
complete AS (
 SELECT e.epoch FROM epochs e JOIN ep USING(epoch)
 WHERE len(e.expected_names)>0 AND list_sort(e.expected_names)=list_sort(e.observed_names)
 AND NOT EXISTS (
  SELECT 1 FROM unnest(e.expected_names) n(name)
  WHERE (SELECT count(*) FROM epoch_endpoints z JOIN servers s USING(server_id)
   WHERE z.epoch=e.epoch AND z.endpoint='LEAFZ' AND z.state='ok' AND s.server_name=n.name)<>1)
),
all_pairs AS (
 SELECT epoch AS a, started_at AS a_at, lead(epoch) OVER(ORDER BY epoch) AS b,
 lead(started_at) OVER(ORDER BY epoch) AS b_at FROM ep),
qualified_pairs AS (
 SELECT p.a,p.a_at,p.b FROM all_pairs p JOIN epochs ea ON ea.epoch=p.a JOIN epochs eb ON eb.epoch=p.b
 WHERE p.b IS NOT NULL AND p.b_at>p.a_at AND date_diff('microsecond',p.a_at,p.b_at)<=?
 AND p.a IN (SELECT epoch FROM complete) AND p.b IN (SELECT epoch FROM complete)
 AND list_sort(ea.expected_names)=list_sort(eb.expected_names)
 AND NOT EXISTS (
  SELECT 1 FROM epoch_endpoints za JOIN servers s USING(server_id)
  WHERE za.epoch=p.a AND za.endpoint='LEAFZ' AND za.state='ok'
  AND (s.start_time>p.a_at OR NOT EXISTS (SELECT 1 FROM epoch_endpoints zb
   WHERE zb.epoch=p.b AND zb.server_id=za.server_id AND zb.endpoint='LEAFZ' AND zb.state='ok')))
),
pairs AS (SELECT * FROM qualified_pairs WHERE a_at>=?),`+leafhealth.Counts(true)+`,
last_seen AS (SELECT site,account,max(epoch) AS last FROM site GROUP BY ALL),
evidence AS (
 SELECT l.site,l.account,l.last,c.drops,
  NOT EXISTS(SELECT 1 FROM site n WHERE n.epoch=? AND n.site=l.site AND n.account=l.account)
  AND (SELECT count(*) FROM ep WHERE epoch>l.last)>=2
  AND NOT EXISTS(SELECT 1 FROM all_pairs p WHERE p.a>=l.last AND p.b IS NOT NULL
   AND NOT EXISTS(SELECT 1 FROM qualified_pairs good WHERE good.a=p.a)) AS absent
 FROM last_seen l LEFT JOIN leaf_counts c USING(site,account)
)
SELECT ev.site, ev.account, ev.drops, ev.absent, ev.last, (SELECT started_at FROM ep WHERE epoch=ev.last), status.eligible, status.recent
FROM (SELECT EXISTS(SELECT 1 FROM qualified_pairs WHERE b=?)
 -- An old sighting separated from continued absence by uncertain coverage
 -- cannot prove either a new drop or recovery of an existing warning.
 AND NOT EXISTS(SELECT 1 FROM evidence u WHERE u.site<>'' AND NOT u.absent
  AND NOT EXISTS(SELECT 1 FROM site n WHERE n.epoch=? AND n.site=u.site AND n.account=u.account)
  AND (SELECT count(*) FROM ep WHERE epoch>u.last)>=2) AS eligible, EXISTS(SELECT 1 FROM qualified_pairs WHERE b=?) AS recent) status
LEFT JOIN evidence ev ON ev.site<>'' AND (ev.absent OR (ev.drops>=3 AND NOT EXISTS(
 SELECT 1 FROM site n WHERE n.site=ev.site AND n.account=ev.account GROUP BY epoch HAVING count(*)>1)))
ORDER BY ev.site,ev.account`, at.Add(-24*time.Hour), epoch, (3 * e.Interval).Microseconds(), at.Add(-time.Hour), epoch, epoch, epoch, epoch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var details []string
	eligible, recent := false, false
	for rows.Next() {
		var site, account sql.NullString
		var drops sql.NullInt64
		var absent sql.NullBool
		var last sql.NullInt64
		var lastAt sql.NullTime
		if err := rows.Scan(&site, &account, &drops, &absent, &last, &lastAt, &eligible, &recent); err != nil {
			return nil, err
		}
		if !recent || !site.Valid {
			continue
		}
		state := fmt.Sprintf("%d observed drops in 1h", drops.Int64)
		if absent.Bool {
			state = "absent for at least two fully observed collections; " + state
		}
		details = append(details, fmt.Sprintf("%q (account %q): %s; last observed at %s UTC (collection %d)", site.String, account.String, state, lastAt.Time.UTC().Format("2006-01-02 15:04:05"), last.Int64))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if !eligible {
		held, err := carryLeafHealth(ctx, conn, epoch)
		if err != nil || len(held) > 0 || !recent {
			return held, err
		}
	}

	if len(details) == 0 {
		return nil, nil
	}
	value := float64(len(details))
	return []store.FindingRow{{EntityKey: "", Severity: "warning", Value: &value, Detail: "Leaf sites: " + strings.Join(details, "; ") + ". Observations do not identify a responsible hub."}}, nil
}

// Carry the immediately previous firing system finding through uncertainty.
// Fresh confirmed evidence may still fire when no earlier warning exists.
func carryLeafHealth(ctx context.Context, conn *sql.Conn, epoch int64) ([]store.FindingRow, error) {
	var detail string
	var value sql.NullFloat64
	err := conn.QueryRowContext(ctx, `SELECT detail,value FROM findings WHERE check_code=? AND state='firing'
 AND last_seen_epoch=(SELECT max(epoch) FROM epochs WHERE epoch<?)`, LeafHealthCode, epoch).Scan(&detail, &value)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var v *float64
	if value.Valid {
		v = &value.Float64
	}
	return []store.FindingRow{{EntityKey: "", Severity: "warning", Value: v, Detail: strings.TrimSuffix(detail, leafUnknownSuffix) + leafUnknownSuffix}}, nil
}

//go:build !(js && wasm)

// Left out of WebAssembly builds because it reads the store; see engine.go.

package checks

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/leafhealth"
	"github.com/shadone/nats-lens/internal/store"
)

const leafUnknownSuffix = " Current leaf observation coverage is uncertain; retaining earlier evidence until collection coverage establishes recovery or the last observation leaves the 24h window."

// leafHealth uses one bounded SQL batch over 24 hours, plus one previous-finding
// read on uncertainty, independent of the site count. Only per-site evidence
// leaves DuckDB. All timestamps are compared directly to columns (host TZ safe).
func (e *Engine) leafHealth(ctx context.Context, conn *sql.Conn, epoch int64, at time.Time) ([]store.FindingRow, error) {
	if e.Interval <= 0 {
		held, err := carryLeafHealth(ctx, conn, epoch, at)
		return leafHealthRows(held, true), err
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
		details = append(details, fmt.Sprintf("%q (account %q): %s; last observed at %s UTC (collection %d)", site.String, account.String, state, lastAt.Time.UTC().Format("2006-01-02 15:04:05.999999999"), last.Int64))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	unknown := false
	if !eligible {
		held, err := carryLeafHealth(ctx, conn, epoch, at)
		if err != nil {
			return nil, err
		}
		unknown = len(held) > 0
		// Keep each original evidence timestamp. Fresh evidence for another site
		// must neither renew an old site's age nor disappear behind a carried row.
		details = mergeLeafEvidence(details, held)
	}
	return leafHealthRows(details, unknown), nil
}

func leafHealthRows(details []string, unknown bool) []store.FindingRow {
	if len(details) == 0 {
		return nil
	}
	value := float64(len(details))
	detail := "Leaf sites: " + strings.Join(details, "; ") + ". Observations do not identify a responsible hub."
	if unknown {
		detail += leafUnknownSuffix
	}
	return []store.FindingRow{{EntityKey: "", Severity: "warning", Value: &value, Detail: detail}}
}

// The code-owned detail is durable per-site evidence in the existing schema.
// Match quoted strings (including escaped quotes/semicolons) and require a full
// round trip before using it. Unknown formats fail the check, not resolve it.
// The observation timestamp is never the finding's refreshed last_seen_epoch.
var leafEvidencePattern = regexp.MustCompile(`("(?:[^"\\]|\\.)*" \(account "(?:[^"\\]|\\.)*"\)): (?:absent for at least two fully observed collections; )?[0-9]+ observed drops in 1h; last observed at ([0-9:. -]+) UTC \(collection [0-9]+\)`)

func mergeLeafEvidence(fresh, held []string) []string {
	byIdentity := make(map[string]string, len(fresh)+len(held))
	for _, entries := range [][]string{held, fresh} {
		for _, entry := range entries {
			byIdentity[leafEvidencePattern.FindStringSubmatch(entry)[1]] = entry
		}
	}
	keys := make([]string, 0, len(byIdentity))
	for key := range byIdentity {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, byIdentity[key])
	}
	return result
}

// Carry only from this check's latest successful execution before epoch.
// Disabled checks resolve in the store, so re-enabling cannot revive carry.
// Mutable current/future finding state is deliberately excluded, as for churn.
func carryLeafHealth(ctx context.Context, conn *sql.Conn, epoch int64, at time.Time) ([]string, error) {
	var detail string
	err := conn.QueryRowContext(ctx, `SELECT detail FROM findings WHERE check_code=? AND state='firing'
 AND updated_epoch<? AND last_seen_epoch=(SELECT max(epoch) FROM check_runs
 WHERE check_code=? AND status='ok' AND epoch<?)`, LeafHealthCode, epoch, LeafHealthCode, epoch).Scan(&detail)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	body := strings.TrimSuffix(strings.TrimSuffix(detail, leafUnknownSuffix), ". Observations do not identify a responsible hub.")
	body = strings.TrimPrefix(body, "Leaf sites: ")
	matches := leafEvidencePattern.FindAllStringSubmatch(body, -1)
	all, kept := make([]string, 0, len(matches)), make([]string, 0, len(matches))
	for _, match := range matches {
		all = append(all, match[0])
		observed, err := time.Parse("2006-01-02 15:04:05", match[2])
		if err != nil {
			return nil, fmt.Errorf("leaf evidence timestamp: %w", err)
		}
		if !observed.Before(at.Add(-24*time.Hour)) && !observed.After(at) {
			kept = append(kept, match[0])
		}
	}
	if len(all) == 0 || strings.Join(all, "; ") != body {
		return nil, fmt.Errorf("unrecognized stored leaf evidence")
	}
	return kept, nil
}

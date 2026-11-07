package storeprov

import (
	"context"
	"fmt"

	"github.com/shadone/nats-lens/internal/api"
)

// replayClientLibraries reads two endpoint rollups, never individual connection
// rows or one query per application. ASVS 1.2.4: values remain bound parameters.
func (p *Provider) replayClientLibraries(ctx context.Context, f *api.ReplayFacts, _ map[string]string) error {
	c := &api.ReplayClientLibraries{}
	f.ClientLibraries = c
	if f.EpochA == f.EpochB && f.EpochA != 0 {
		c.Unavailable = "A and B resolve to the same collection; two distinct observations are required."
		return nil
	}
	if f.EpochA == 0 || f.EpochB == 0 {
		c.Unavailable = "No collection near A or B."
		return nil
	}
	// Require every expected server's CONNZ and uncapped, known totals. Other
	// endpoint failures do not invalidate CONNZ; process IDs may change at B.
	var complete bool
	err := p.db().QueryRowContext(ctx, `SELECT count(*)=2 AND coalesce(bool_and(
   expected_servers > 0 AND len(missing_names)=0 AND
   (SELECT count(DISTINCT s.server_name) FROM epoch_endpoints x JOIN servers s USING(server_id)
     JOIN server_stats z ON z.epoch=x.epoch AND z.server_id=x.server_id
     WHERE x.epoch=e.epoch AND x.endpoint='CONNZ' AND x.state='ok'
       AND list_contains(e.expected_names,s.server_name)
       AND z.connz_total IS NOT NULL AND z.connz_returned IS NOT NULL AND z.connz_total=z.connz_returned)=expected_servers
 ),false) FROM epochs e WHERE epoch IN (?,?)`, f.EpochA, f.EpochB).Scan(&complete)
	if err != nil {
		return fmt.Errorf("replay client coverage: %w", err)
	}
	if !complete {
		c.Unavailable = "CONNZ coverage at A or B is missing, incomplete, capped, or has unknown totals."
		return nil
	}
	tooling, args := p.eventToolingSQL("c.name", "c.ip")
	args = append(args, f.EpochA, f.EpochB, replayRowLimit+1)
	rows, err := p.db().QueryContext(ctx, `SELECT c.epoch,
 coalesce(nullif(split_part(coalesce(c.name,''),'@',1),''),'(unnamed)'),
 coalesce(nullif(c.account,''),'$G'), coalesce(c.lang,''),coalesce(c.version,''),count(*)
 FROM connections c JOIN epochs x ON x.epoch=c.epoch
 WHERE NOT `+tooling+` AND c.epoch IN (?,?)
 GROUP BY ALL ORDER BY ALL LIMIT ?`, args...)
	if err != nil {
		return fmt.Errorf("replay client libraries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var l api.ReplayClientLibrary
		if err := rows.Scan(&l.Epoch, &l.App, &l.Account, &l.Lang, &l.Version, &l.Connections); err != nil {
			return err
		}
		c.Rows = append(c.Rows, l)
	}
	if len(c.Rows) > replayRowLimit {
		c.Rows = nil
		c.Unavailable = "Client-library rollup read limit reached; no comparison is made."
	}
	return rows.Err()
}

package storeprov

// capacityStoreSQL preserves unknown observations through bucket reduction.
// Names continue across restarts; concurrent processes sharing a name cannot
// be assigned to one card. Disabled JetStream is known zero file-store usage.
func capacityStoreSQL(server bool) string {
	filter := ""
	if server {
		filter = " AND ss.server_id IN (SELECT server_id FROM servers WHERE server_name = ?)"
	}
	per := `
WITH ep AS (SELECT epoch, started_at FROM epochs WHERE started_at >= ? AND started_at < ?)
SELECT e.epoch, e.started_at AS t, s.server_name,
 CASE WHEN count(ss.server_id) = 1 THEN
   max(CASE WHEN ss.js_enabled = false THEN 0 ELSE ss.js_storage END)
 END AS v
FROM ep e LEFT JOIN server_stats ss ON ss.epoch = e.epoch` + filter + `
LEFT JOIN servers s USING (server_id)
GROUP BY e.epoch, e.started_at, s.server_name`
	if server {
		return `SELECT t, v FROM (` + per + `)`
	}
	return `SELECT p.t, CASE WHEN count(p.v) = count(*)
 AND count(*) >= max(e.expected_servers) AND count(*) >= max(e.observed_servers)
 THEN sum(p.v) END AS v FROM (` + per + `) p JOIN epochs e USING (epoch)
 GROUP BY p.epoch, p.t`
}

package storeprov

// capacityStoreSQL preserves an unknown observation as NULL until bucket
// reduction. Server names continue across process restarts, but simultaneous
// processes with the same name cannot be assigned to one server card.
func capacityStoreSQL(server bool) string {
	filter := ""
	if server {
		filter = " AND s.server_name = ?"
	}
	per := `
SELECT e.epoch, e.started_at AS t, s.server_name,
 CASE WHEN count(*) = 1 AND count(ss.js_storage) = 1
 THEN max(ss.js_storage) END AS v
FROM epochs e JOIN server_stats ss USING (epoch) JOIN servers s USING (server_id)
WHERE e.started_at >= ? AND e.started_at < ?` + filter + `
GROUP BY e.epoch, e.started_at, s.server_name`
	if server {
		return `SELECT t, v FROM (` + per + `)`
	}
	return `SELECT p.t, CASE WHEN count(p.v) = count(*)
 AND count(*) >= max(e.expected_servers) AND count(*) >= max(e.observed_servers)
 THEN sum(p.v) END AS v FROM (` + per + `) p JOIN epochs e USING (epoch)
 GROUP BY p.epoch, p.t`
}

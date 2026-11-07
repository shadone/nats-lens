// Package leafhealth shares Topology's stored leaf connection-drop rule with checks.
package leafhealth

// Counts builds CTEs on ep and pairs (a, a_at, b), followed by leaf_counts.
// Page counts include connection moves. Checks exclude handoffs to another hub.
// Only fixed SQL fragments are composed; values remain bound (ASVS 1.2.4).
func Counts(excludeHandoffs bool) string {
	endFilter, gapFilter := "", ""
	if excludeHandoffs {
		filter := func(alias string) string {
			return " AND NOT EXISTS (SELECT 1 FROM site other WHERE other.epoch=p.b AND other.site=" + alias + ".site AND other.account=" + alias + ".account AND other.server_id<>" + alias + ".server_id)"
		}
		endFilter, gapFilter = filter("c"), filter("l")
	}
	return `site AS (SELECT epoch, server_id, lid, coalesce(nullif(name, ''), ip, '') AS site, coalesce(account, '') AS account
  FROM leafnodes WHERE NOT coalesce(is_spoke, false) AND epoch IN (SELECT epoch FROM ep)),
ok AS (SELECT DISTINCT epoch, server_id FROM epoch_endpoints
  WHERE endpoint = 'LEAFZ' AND state = 'ok' AND epoch IN (SELECT epoch FROM ep)),
-- The rows of a connection observed between consecutive collections.
seen AS (
  SELECT l.site, l.account, count(*) AS n FROM pairs p JOIN site l ON l.epoch = p.a
  JOIN ok oa ON oa.epoch = p.a AND oa.server_id = l.server_id
  JOIN ok ob ON ob.epoch = p.b AND ob.server_id = l.server_id
  GROUP BY ALL),
pos AS (SELECT epoch, row_number() OVER (ORDER BY epoch) AS i FROM ep),
-- Each connection with its rows: one stored once in every collection
-- from its first to its last can only have dropped after its last; a
-- duplicate row sends it to the row-by-row count, so it cannot hide a gap.
conns AS (
  SELECT c.*, c.n = c.d AND c.d = pl.i - pf.i + 1 AS unbroken FROM (
    SELECT server_id, lid, site, account, count(*) AS n, count(DISTINCT epoch) AS d, min(epoch) AS first, max(epoch) AS last
    FROM site GROUP BY ALL) c
  JOIN pos pf ON pf.epoch = c.first JOIN pos pl ON pl.epoch = c.last),
ends AS (
  SELECT c.site, c.account, count(*) AS drops FROM conns c JOIN pairs p ON p.a = c.last
  JOIN ok oa ON oa.epoch = p.a AND oa.server_id = c.server_id
  JOIN ok ob ON ob.epoch = p.b AND ob.server_id = c.server_id
  WHERE c.unbroken ` + endFilter + ` GROUP BY ALL),
-- The other connections, rare, row by row.
broken AS (
  SELECT s.*, min(epoch) OVER (PARTITION BY server_id, lid, site, account ORDER BY epoch RANGE BETWEEN 1 FOLLOWING AND UNBOUNDED FOLLOWING) AS nxt FROM site s
  WHERE (server_id, lid, site, account) IN (SELECT (server_id, lid, site, account) FROM conns WHERE NOT unbroken)),
gaps AS (
  SELECT l.site, l.account, count(*) FILTER (WHERE l.nxt IS DISTINCT FROM p.b ` + gapFilter + `) AS drops
  FROM pairs p JOIN broken l ON l.epoch = p.a
  JOIN ok oa ON oa.epoch = p.a AND oa.server_id = l.server_id
  JOIN ok ob ON ob.epoch = p.b AND ob.server_id = l.server_id
  GROUP BY ALL),
leaf_counts AS (
SELECT s.site, s.account, coalesce(e.drops, 0) + coalesce(g.drops, 0) AS drops
FROM seen s LEFT JOIN ends e USING (site, account) LEFT JOIN gaps g USING (site, account))`
}

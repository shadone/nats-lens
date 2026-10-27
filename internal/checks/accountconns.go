package checks

import "strings"

// accountServersSQL is the query of AccountServersSQL, with {ep} for the
// epoch.
const accountServersSQL = `SELECT st.account, p.server_id, p.server_name, st.conns, st.leafnodes, st.subscriptions
FROM account_stats st
JOIN (
    SELECT x.server_id, coalesce(s.server_name, x.server_id) AS server_name
    FROM (SELECT DISTINCT server_id FROM account_stats WHERE epoch = {ep}) x
    LEFT JOIN servers s ON s.server_id = x.server_id
    LEFT JOIN server_stats ss ON ss.epoch = {ep} AND ss.server_id = x.server_id
    QUALIFY row_number() OVER (PARTITION BY coalesce(s.server_name, x.server_id)
        ORDER BY coalesce(ss.start_time, s.start_time) DESC NULLS LAST, x.server_id DESC) = 1
) p ON p.server_id = st.server_id
WHERE st.epoch = {ep}`

// accountConnectionsSQL is the query of AccountConnectionsSQL, with
// {servers} for the query of AccountServersSQL.
const accountConnectionsSQL = `SELECT account, sum(conns)::BIGINT AS clients, sum(leafnodes)::BIGINT AS leafnodes,
    sum(subscriptions)::BIGINT AS subscriptions, max(leafnodes)::BIGINT AS limit_leafnodes,
    CASE WHEN max(leafnodes) > 0 THEN arg_max(server_name, (coalesce(leafnodes, 0), server_name)) END AS limit_leafnodes_server
FROM ({servers})
GROUP BY account`

// AccountServersSQL is a query of each account's STATZ share on each
// server of epoch ep, a SQL expression of the epoch: account, server_id,
// server_name (the server ID when the name is not known), conns,
// leafnodes and subscriptions as the server reported them. Of two
// processes of one server name in the epoch (the old one shutting down,
// the new one up) only one is read, the later started, then the larger
// server ID, as server-016 and the Accounts page read certificates; else
// the server's clients would count twice.
func AccountServersSQL(ep string) string {
	return strings.ReplaceAll(accountServersSQL, "{ep}", ep)
}

// AccountConnectionsSQL is a query of each account's connections in epoch
// ep as nats-server counts them against the account JWT's connection
// limit, from AccountServersSQL: a server refuses a new client of the
// account once the client connections of every server (STATZ shares) plus
// its own leaf node connections reach the limit. Columns: account; clients,
// leafnodes and subscriptions summed over the servers, NULL when no server
// reported them; limit_leafnodes, the leaf nodes of the server that holds
// the most, the first to refuse; limit_leafnodes_server, that server, the
// larger name on a tie, NULL without leaf nodes. account-001 and the
// Accounts page both read it, so the check and the page count alike.
func AccountConnectionsSQL(ep string) string {
	return strings.ReplaceAll(accountConnectionsSQL, "{servers}", AccountServersSQL(ep))
}

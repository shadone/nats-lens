package checks

import "strings"

// accountServersSQL is the query of AccountServersSQL, with {ep} for the
// epoch.
const accountServersSQL = `SELECT st.account, st.server_id, coalesce(s.server_name, st.server_id) AS server_name, st.conns,
    st.leafnodes, st.subscriptions
FROM account_stats st LEFT JOIN servers s ON s.server_id = st.server_id
WHERE st.epoch = {ep}`

// accountConnectionsSQL is the query of AccountConnectionsSQL, with
// {servers} for the query of AccountServersSQL.
const accountConnectionsSQL = `SELECT account, sum(conns)::BIGINT AS clients, sum(leafnodes)::BIGINT AS leafnodes,
    sum(subscriptions)::BIGINT AS subscriptions, max(leafnodes)::BIGINT AS limit_leafnodes,
    CASE WHEN max(leafnodes) > 0 THEN arg_max(server_name, (coalesce(leafnodes, 0), server_name)) END AS limit_leafnodes_server
FROM ({servers})
GROUP BY account`

// AccountServersSQL is a query of each account's STATZ share on each
// server process of epoch ep, a SQL expression of the epoch: account,
// server_id, server_name (the server ID when the name is not known), conns,
// leafnodes and subscriptions as the process reported them. Two processes
// of one server name in the epoch (the old one shutting down, the new one
// up) are both read: nats-server keeps each remote server's connections by
// server ID until that process shuts down and counts them all against the
// limit (MaxTotalConnectionsReached), and ACCSTATZ is one request, so both
// processes that answered it were up.
func AccountServersSQL(ep string) string {
	return strings.ReplaceAll(accountServersSQL, "{ep}", ep)
}

// AccountConnectionsSQL is a query of each account's connections in epoch
// ep as nats-server counts them against the account JWT's connection
// limit, from AccountServersSQL: a server refuses a new client of the
// account once the client connections of every server (STATZ shares) plus
// its own leaf node connections reach the limit. Columns: account; clients,
// leafnodes and subscriptions summed over the server processes, NULL when
// none reported them; limit_leafnodes, the leaf nodes of the process that
// holds the most, the first to refuse; limit_leafnodes_server, its server
// name, the larger name on a tie, NULL without leaf nodes. account-001 and the
// Accounts page both read it, so the check and the page count alike.
func AccountConnectionsSQL(ep string) string {
	return strings.ReplaceAll(accountConnectionsSQL, "{servers}", AccountServersSQL(ep))
}

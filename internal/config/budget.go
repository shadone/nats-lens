package config

// ThreadsPerQuery is the share of a database's threads the derived query
// budget gives each concurrent API query: with 12 threads, 4 queries. The
// API's throughput stopped growing at about four concurrent callers on 12
// threads (docs/operations/performance-baseline.md). Proposed, not yet
// confirmed.
const ThreadsPerQuery = 3

// QueryBudgetFor returns the query budget of a database with threads
// threads: query_budget when set, otherwise one query per ThreadsPerQuery
// threads, at least one.
func (c Config) QueryBudgetFor(threads int) int {
	if c.QueryBudget > 0 {
		return c.QueryBudget
	}
	return max(1, threads/ThreadsPerQuery)
}

package config

// ThreadsPerQuery is the share of a database's threads the derived query
// budget gives each concurrent API query: with 12 threads, 4 queries. The
// API's throughput stopped growing at about four concurrent callers on 12
// threads (docs/operations/performance-baseline.md). Proposed, not yet
// confirmed.
const ThreadsPerQuery = 3

// QueryBudgetFor returns the query budget of a database with threads
// threads: query_budget when set, 0 when it is -1 (no budget), otherwise
// one query per ThreadsPerQuery threads, at least one.
func (c Config) QueryBudgetFor(threads int) int {
	switch {
	case c.QueryBudget > 0:
		return c.QueryBudget
	case c.QueryBudget < 0:
		return 0
	}
	return max(1, threads/ThreadsPerQuery)
}

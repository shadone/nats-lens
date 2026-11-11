package perf

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/shadone/nats-lens/internal/timing"
)

// queryCost is one SQL text's cost within one procedure.
type queryCost struct {
	Procedure, Source, SQL string
	Calls                  int
	Total, Max             time.Duration
	// Args are the arguments of the slowest run, which Explain replays.
	Args []any
}

// Explain opens the store and, after one unrecorded Overview to warm the
// buffers, records every query of three Overviews, each other page once
// and two rounds of an agent, with its text and arguments. It writes
// summary.md, ranking procedures and queries by query time, and one file
// per heavy query with its EXPLAIN ANALYZE: the ten costliest queries and
// the costliest of each of the five costliest procedures.
func Explain(ctx context.Context, path string, p Profile, dir string, log *slog.Logger) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	h, closeAll, err := openHarness(ctx, LoadOptions{Profile: p, Path: path, Log: log})
	if err != nil {
		return err
	}
	defer closeAll()
	if err := h.loadStreams(ctx); err != nil {
		return err
	}
	rec := &recorder{}
	h.loadPage(ctx, rec, overviewPage(h.now, day))

	var mu sync.Mutex
	costs := map[string]*queryCost{}
	timing.SetQueryHook(func(q timing.QueryInfo) {
		if q.Procedure == "" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		k := q.Procedure + "\x00" + q.SQL
		c := costs[k]
		if c == nil {
			c = &queryCost{Procedure: q.Procedure, Source: q.Source, SQL: q.SQL}
			costs[k] = c
		}
		c.Calls++
		c.Total += q.Took
		if q.Took >= c.Max {
			c.Max, c.Args = q.Took, q.Args
		}
	})
	for range 3 {
		h.loadPage(ctx, rec, overviewPage(h.now, day))
	}
	for _, pg := range tabPages(h.now, day) {
		h.loadPage(ctx, rec, pg)
	}
	for range 2 {
		for _, c := range agentCalls(h.now, day, h.streams, 0) {
			h.loadPage(ctx, rec, page{name: c.proc, calls: []call{c}})
		}
	}
	timing.SetQueryHook(nil)

	all := make([]*queryCost, 0, len(costs))
	byProc := map[string]time.Duration{}
	for _, c := range costs {
		all = append(all, c)
		byProc[c.Procedure] += c.Total
	}
	slices.SortFunc(all, func(a, b *queryCost) int { return cmp.Compare(b.Total, a.Total) })
	procs := make([]string, 0, len(byProc))
	for pr := range byProc {
		procs = append(procs, pr)
	}
	slices.SortFunc(procs, func(a, b string) int { return cmp.Compare(byProc[b], byProc[a]) })

	pick := map[*queryCost]bool{}
	var picked []*queryCost
	add := func(c *queryCost) {
		if !pick[c] && isRead(c.SQL) {
			pick[c] = true
			picked = append(picked, c)
		}
	}
	for _, c := range all[:min(10, len(all))] {
		add(c)
	}
	for _, pr := range procs[:min(5, len(procs))] {
		for _, c := range all {
			if c.Procedure == pr && isRead(c.SQL) {
				add(c)
				break
			}
		}
	}

	var sum strings.Builder
	fmt.Fprintf(&sum, "# Query costs\n\nProfile %s; store %s.\n\n| Procedure | Distinct queries | Query time ms |\n|---|---|---|\n", p.Name, filepath.Base(path))
	for _, pr := range procs {
		n := 0
		for _, c := range all {
			if c.Procedure == pr {
				n++
			}
		}
		fmt.Fprintf(&sum, "| %s | %d | %s |\n", pr, n, ms(byProc[pr]))
	}
	sum.WriteString("\n| # | Procedure | Calls | Total ms | Max ms | Plan | First line |\n|---|---|---|---|---|---|---|\n")
	for i, c := range all[:min(25, len(all))] {
		plan := ""
		if pick[c] {
			plan = planFile(slices.Index(picked, c), c)
		}
		fmt.Fprintf(&sum, "| %d | %s | %d | %s | %s | %s | `%s` |\n", i+1, c.Procedure, c.Calls, ms(c.Total), ms(c.Max), plan,
			strings.ReplaceAll(firstSQLLine(c.SQL), "|", "\\|"))
	}
	if err := os.WriteFile(filepath.Join(dir, "summary.md"), []byte(sum.String()), 0o600); err != nil {
		return err
	}
	for i, c := range picked {
		if err := h.explainOne(ctx, filepath.Join(dir, planFile(i, c)), c); err != nil {
			log.Warn("explain failed", "procedure", c.Procedure, "err", err)
		}
	}
	return nil
}

func planFile(i int, c *queryCost) string { return fmt.Sprintf("%02d-%s.txt", i+1, c.Procedure) }

func isRead(q string) bool {
	q = strings.ToUpper(strings.TrimSpace(q))
	return strings.HasPrefix(q, "SELECT") || strings.HasPrefix(q, "WITH") || strings.HasPrefix(q, "FROM")
}

func firstSQLLine(q string) string {
	q = strings.Join(strings.Fields(q), " ")
	if len(q) > 120 {
		q = q[:120] + "..."
	}
	return q
}

// explainOne replays a query with its slowest arguments under EXPLAIN
// ANALYZE and writes the query, its cost and the plan.
func (h *harness) explainOne(ctx context.Context, path string, c *queryCost) error {
	var b strings.Builder
	fmt.Fprintf(&b, "procedure: %s\nsource: %s\ncalls: %d\ntotal_ms: %s\nmax_ms: %s\nargs: %v\n\n%s\n\n", c.Procedure, c.Source, c.Calls,
		ms(c.Total), ms(c.Max), c.Args, c.SQL)
	start := time.Now()
	rows, err := h.st.DB().QueryContext(ctx, "EXPLAIN ANALYZE "+c.SQL, c.Args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return err
		}
		b.WriteString(v)
		b.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		return err
	}
	fmt.Fprintf(&b, "\nreplay_ms: %s\n", ms(time.Since(start)))
	h.planJSON = strings.TrimSuffix(path, ".txt") + ".json"
	if top, err := h.topOperators(ctx, c); err != nil {
		fmt.Fprintf(&b, "\ntop operators: %v\n", err)
	} else {
		b.WriteString("\ntop operators by time:\n")
		for _, o := range top {
			fmt.Fprintf(&b, "  %8.1f ms  %12d rows  %s %s\n", o.ms, o.rows, o.name, o.extra)
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

type operator struct {
	name, extra string
	ms          float64
	rows        int64
}

// topOperators replays the query under EXPLAIN (ANALYZE, FORMAT json)
// and returns its six slowest operators.
func (h *harness) topOperators(ctx context.Context, c *queryCost) ([]operator, error) {
	var k, v string
	if err := h.st.DB().QueryRowContext(ctx, "EXPLAIN (ANALYZE, FORMAT json) "+c.SQL, c.Args...).Scan(&k, &v); err != nil {
		return nil, err
	}
	if h.planJSON != "" {
		_ = os.WriteFile(h.planJSON, []byte(v), 0o600)
	}
	var root any
	if err := json.Unmarshal([]byte(v), &root); err != nil {
		return nil, err
	}
	var ops []operator
	var walk func(any)
	walk = func(n any) {
		switch t := n.(type) {
		case []any:
			for _, x := range t {
				walk(x)
			}
		case map[string]any:
			if name, ok := t["type"].(string); ok {
				o := operator{name: name}
				if f, ok := t["timing"].(float64); ok {
					o.ms = f * 1000
				}
				if f, ok := t["intermediate_rows"].(float64); ok {
					o.rows = int64(f)
				}
				if ex, ok := t["extra_info"].(map[string]any); ok {
					for _, key := range []string{"Table", "Join Type", "Conditions", "Aggregates", "Groups", "CTE Name"} {
						if s, ok := ex[key]; ok {
							o.extra += fmt.Sprintf("%s=%v ", key, s)
						}
					}
				}
				ops = append(ops, o)
			}
			walk(t["operator"])
			for _, key := range []string{"children"} {
				walk(t[key])
			}
		}
	}
	walk(root)
	slices.SortFunc(ops, func(a, b operator) int { return cmp.Compare(b.ms, a.ms) })
	return ops[:min(6, len(ops))], nil
}

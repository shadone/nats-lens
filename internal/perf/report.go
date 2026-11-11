package perf

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/timing"
)

// Report is the outcome of one load run.
type Report struct {
	Env     Env
	Results []ScenarioResult
}

// Env records what the numbers depend on.
type Env struct {
	Profile    string
	StoreBytes int64
	OpenTook   time.Duration
	// FillTook is the asset count fill after the open, apart from OpenTook.
	FillTook     time.Duration
	GoVersion    string
	DuckDB       string
	Threads      string
	MemoryLimit  string
	CPUs         int
	Host         string
	LoadAtStart  string
	GeneratedAt  time.Time
	EpochsStored int64
	// QueryBudget is the store's query budget; 0 for none.
	QueryBudget int
}

// ScenarioResult is one scenario's numbers.
type ScenarioResult struct {
	Name        string
	Description string
	Elapsed     time.Duration
	LoadBefore  string
	LoadAfter   string
	// Client side: every call and every page load as the client saw it.
	Procedures []ProcStat
	Pages      []PageStat
	// Server side, from the timing histograms' change over the scenario.
	Server       ServerStats
	Background   *Background `json:",omitempty"`
	MaxHeapBytes uint64
	MaxRSSBytes  int64
	// The most queries that held, and that waited for, a slot of the
	// query budget at once, and the most database connections open and in
	// use, sampled every 50 ms; ConnWaits counts the waits for a pooled
	// connection.
	MaxBudgetActive, MaxBudgetQueued int
	MaxConnsOpen, MaxConnsInUse      int
	ConnWaits                        int64
}

// ProcStat is one procedure's calls in a scenario.
type ProcStat struct {
	Procedure     string
	Calls, Errors int
	FirstError    string `json:",omitempty"`
	P50, P95, Max time.Duration
	// ErrorP50 is the median time to a failed call's answer.
	ErrorP50           time.Duration
	PerSecond          float64
	QueriesPerCall     float64
	QuerySecondsPerCal float64
}

// PageStat is the time from a page's first call to its last answer.
type PageStat struct {
	Page          string
	Loads         int
	P50, P95, Max time.Duration
}

// ServerStats are histogram changes over the scenario.
type ServerStats struct {
	Lock      []HistStat
	Jobs      []HistStat
	Queries   []HistStat
	Admission []HistStat
}

// HistStat summarises one histogram's change: its count, mean, and the
// bucket bounds that hold its median and 95th percentile.
type HistStat struct {
	Family, Labels string
	Count          uint64
	Mean           time.Duration
	P50, P95       time.Duration
}

// Background is what the epoch writer and the archive did.
type Background struct {
	EpochInterval time.Duration
	// Missed counts epochs stored late or not at all; Lost those whose
	// write failed.
	Epochs, Missed, Lost         int
	WriteP50, WriteP95, WriteMax time.Duration
	ChecksP50, ChecksMax         time.Duration
	// Lag is from an epoch's tick until its checks finished.
	LagP50, LagMax                 time.Duration
	ArchivedHours                  int
	ArchiveTickP50, ArchiveTickMax time.Duration
	Errors                         []string `json:",omitempty"`
}

func loadAverage() string {
	out, err := exec.Command("sysctl", "-n", "vm.loadavg").Output()
	if err != nil {
		b, err := os.ReadFile("/proc/loadavg")
		if err != nil {
			return "unknown"
		}
		out = b
	}
	return strings.Trim(strings.TrimSpace(string(out)), "{} ")
}

func environment(ctx context.Context, st *store.Store, o LoadOptions, openTook, fillTook time.Duration) Env {
	e := Env{Profile: o.Profile.String(), OpenTook: openTook, FillTook: fillTook, GoVersion: runtime.Version(), CPUs: runtime.NumCPU(),
		LoadAtStart: loadAverage(), GeneratedAt: time.Now().UTC()}
	if fi, err := os.Stat(o.Path); err == nil {
		e.StoreBytes = fi.Size()
	}
	e.Host, _ = os.Hostname()
	e.Host = runtime.GOOS + "/" + runtime.GOARCH
	db := st.DB()
	_ = db.QueryRowContext(ctx, `SELECT version()`).Scan(&e.DuckDB)
	_ = db.QueryRowContext(ctx, `SELECT current_setting('threads')::VARCHAR`).Scan(&e.Threads)
	_ = db.QueryRowContext(ctx, `SELECT current_setting('memory_limit')::VARCHAR`).Scan(&e.MemoryLimit)
	_ = db.QueryRowContext(ctx, `SELECT count(*) FROM epochs`).Scan(&e.EpochsStored)
	if g := st.QueryBudget(); g != nil {
		e.QueryBudget = g.Limit()
	}
	return e
}

// timings is a snapshot of the histograms the report reads.
type timings map[string][]timing.Series

var reportFamilies = map[string]*timing.HistogramVec{
	"api_queries":      timing.APIQueries,
	"api_query_secs":   timing.APIQuerySeconds,
	"lock_wait":        timing.LockWait,
	"lock_held":        timing.LockHeld,
	"job":              timing.JobDuration,
	"query_by_source":  timing.QueryDuration,
	"api_duration_srv": timing.APIDuration,
	"admission":        timing.AdmissionWait,
}

func snapshotTimings() timings {
	t := timings{}
	for k, v := range reportFamilies {
		t[k] = v.Series()
	}
	return t
}

// diffTimings returns after minus before, series by series.
func diffTimings(before, after timings) timings {
	out := timings{}
	for k, ss := range after {
		prev := map[string]timing.Series{}
		for _, s := range before[k] {
			prev[strings.Join(s.Labels, "\x00")] = s
		}
		for _, s := range ss {
			p, ok := prev[strings.Join(s.Labels, "\x00")]
			d := timing.Series{Labels: s.Labels, Bounds: s.Bounds, Buckets: slices.Clone(s.Buckets), Sum: s.Sum, Count: s.Count}
			if ok {
				for i := range d.Buckets {
					d.Buckets[i] -= p.Buckets[i]
				}
				d.Sum -= p.Sum
				d.Count -= p.Count
			}
			if d.Count > 0 {
				out[k] = append(out[k], d)
			}
		}
	}
	return out
}

// bucketQuantile is the upper bound of the bucket holding the q-quantile;
// +Inf is reported as the largest bound.
func bucketQuantile(s timing.Series, q float64) float64 {
	rank := uint64(math.Ceil(q * float64(s.Count)))
	for i, c := range s.Buckets {
		if c >= rank {
			if i < len(s.Bounds) {
				return s.Bounds[i]
			}
			return math.Inf(1)
		}
	}
	return math.Inf(1)
}

func secs(f float64) time.Duration {
	if math.IsInf(f, 1) {
		return -1
	}
	return time.Duration(f * float64(time.Second))
}

func histStats(family string, ss []timing.Series) []HistStat {
	var out []HistStat
	for _, s := range ss {
		out = append(out, HistStat{Family: family, Labels: strings.Join(s.Labels[1:], ","), Count: s.Count,
			Mean: secs(s.Sum / float64(s.Count)), P50: secs(bucketQuantile(s, 0.5)), P95: secs(bucketQuantile(s, 0.95))})
	}
	return out
}

func (r *ScenarioResult) summarize(rec *recorder, d timings) {
	byProc := map[string][]sample{}
	for _, s := range rec.samples {
		byProc[s.proc] = append(byProc[s.proc], s)
	}
	queries := map[string]timing.Series{}
	for _, s := range d["api_queries"] {
		if s.Labels[0] == SystemID || s.Labels[0] == timing.AllSystems {
			queries[s.Labels[1]] = s
		}
	}
	qsecs := map[string]timing.Series{}
	for _, s := range d["api_query_secs"] {
		if s.Labels[0] == SystemID || s.Labels[0] == timing.AllSystems {
			qsecs[s.Labels[1]] = s
		}
	}
	for proc, ss := range byProc {
		var ds, errs []time.Duration
		st := ProcStat{Procedure: proc, Calls: len(ss)}
		for _, s := range ss {
			if s.err != nil {
				errs = append(errs, s.took)
				st.Errors++
				if st.FirstError == "" {
					st.FirstError = s.err.Error()
				}
				continue
			}
			ds = append(ds, s.took)
		}
		st.P50, st.P95, st.Max = percentile(ds, 0.5), percentile(ds, 0.95), percentile(ds, 1)
		st.ErrorP50 = percentile(errs, 0.5)
		st.PerSecond = float64(len(ds)) / r.Elapsed.Seconds()
		if q, ok := queries[proc]; ok && q.Count > 0 {
			st.QueriesPerCall = q.Sum / float64(q.Count)
		}
		if q, ok := qsecs[proc]; ok && q.Count > 0 {
			st.QuerySecondsPerCal = q.Sum / float64(q.Count)
		}
		r.Procedures = append(r.Procedures, st)
	}
	slices.SortFunc(r.Procedures, func(a, b ProcStat) int { return int(b.P95 - a.P95) })
	for name, ds := range rec.pages {
		r.Pages = append(r.Pages, PageStat{Page: name, Loads: len(ds), P50: percentile(ds, 0.5), P95: percentile(ds, 0.95), Max: percentile(ds, 1)})
	}
	slices.SortFunc(r.Pages, func(a, b PageStat) int { return strings.Compare(a.Page, b.Page) })
	r.Server.Lock = append(histStats("wait", filterSystem(d["lock_wait"])), histStats("held", filterSystem(d["lock_held"]))...)
	r.Server.Jobs = histStats("job", filterSystem(d["job"]))
	r.Server.Queries = histStats("query", filterSystem(d["query_by_source"]))
	r.Server.Admission = histStats("admission", filterSystem(d["admission"]))
}

func filterSystem(ss []timing.Series) []timing.Series {
	var out []timing.Series
	for _, s := range ss {
		if s.Labels[0] == SystemID {
			out = append(out, s)
		}
	}
	return out
}

func ms(d time.Duration) string {
	if d < 0 {
		return ">60000"
	}
	return fmt.Sprintf("%.1f", float64(d)/float64(time.Millisecond))
}

// Markdown renders the report as the tables the baseline document holds.
func (r *Report) Markdown() string {
	var b strings.Builder
	e := r.Env
	fmt.Fprintf(&b, "## Run %s\n\n", e.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "- Profile: %s\n- Store: %.2f GB, %d epochs, opened in %s, asset counts filled in %s\n- %s, %s, %d CPUs; DuckDB %s, threads %s, memory_limit %s\n- Query budget: %d (0: none)\n- Load average at start: %s\n\n",
		e.Profile, float64(e.StoreBytes)/1e9, e.EpochsStored, e.OpenTook.Round(time.Millisecond), e.FillTook.Round(time.Millisecond), e.GoVersion, e.Host, e.CPUs, e.DuckDB, e.Threads, e.MemoryLimit, e.QueryBudget, e.LoadAtStart)
	for _, res := range r.Results {
		fmt.Fprintf(&b, "### %s\n\n%s Ran %s; load %s before, %s after; peak Go heap %d MB, peak RSS %d MB. Query budget: at most %d slots held and %d queries waiting; connections: at most %d open and %d in use, %d waits for one.\n\n",
			res.Name, res.Description, res.Elapsed.Round(time.Millisecond), res.LoadBefore, res.LoadAfter, res.MaxHeapBytes>>20, res.MaxRSSBytes>>20,
			res.MaxBudgetActive, res.MaxBudgetQueued, res.MaxConnsOpen, res.MaxConnsInUse, res.ConnWaits)
		b.WriteString("| Procedure | Calls | Errors | error p50 ms | p50 ms | p95 ms | max ms | calls/s | queries/call | query s/call |\n|---|---|---|---|---|---|---|---|---|---|\n")
		for _, p := range res.Procedures {
			fmt.Fprintf(&b, "| %s | %d | %d | %s | %s | %s | %s | %.2f | %.1f | %.3f |\n", p.Procedure, p.Calls, p.Errors, ms(p.ErrorP50), ms(p.P50), ms(p.P95), ms(p.Max), p.PerSecond, p.QueriesPerCall, p.QuerySecondsPerCal)
		}
		for _, p := range res.Procedures {
			if p.FirstError != "" {
				fmt.Fprintf(&b, "\nFirst error of %s: `%s`\n", p.Procedure, firstLine(p.FirstError))
			}
		}
		if len(res.Pages) > 1 || (len(res.Pages) == 1 && len(res.Procedures) > 1) {
			b.WriteString("\n| Page | Loads | p50 ms | p95 ms | max ms |\n|---|---|---|---|---|\n")
			for _, p := range res.Pages {
				fmt.Fprintf(&b, "| %s | %d | %s | %s | %s |\n", p.Page, p.Loads, ms(p.P50), ms(p.P95), ms(p.Max))
			}
		}
		if len(res.Server.Lock)+len(res.Server.Jobs) > 0 {
			b.WriteString("\n| Server timing | Labels | Count | mean ms | p50 bucket ms | p95 bucket ms |\n|---|---|---|---|---|---|\n")
			for _, groups := range [][]HistStat{res.Server.Lock, res.Server.Jobs, res.Server.Queries, res.Server.Admission} {
				for _, h := range groups {
					fmt.Fprintf(&b, "| %s | %s | %d | %s | %s | %s |\n", h.Family, h.Labels, h.Count, ms(h.Mean), ms(h.P50), ms(h.P95))
				}
			}
		}
		if bg := res.Background; bg != nil {
			fmt.Fprintf(&b, "\nEpochs every %s: %d attempted, %d late or lost, %d lost; write p50 %s ms, p95 %s ms, max %s ms; checks p50 %s ms, max %s ms; tick to checks done p50 %s ms, max %s ms. Archive: %d hours exported, tick p50 %s ms, max %s ms.\n",
				bg.EpochInterval, bg.Epochs, bg.Missed, bg.Lost, ms(bg.WriteP50), ms(bg.WriteP95), ms(bg.WriteMax), ms(bg.ChecksP50), ms(bg.ChecksMax), ms(bg.LagP50), ms(bg.LagMax),
				bg.ArchivedHours, ms(bg.ArchiveTickP50), ms(bg.ArchiveTickMax))
			for _, e := range bg.Errors {
				fmt.Fprintf(&b, "\nBackground error: `%s`\n", firstLine(e))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}

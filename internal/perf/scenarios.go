package perf

import (
	"context"
	"slices"
	"sync"
	"time"
)

// scenario drives the harness for one measurement.
type scenario struct {
	description string
	// order places it in ScenarioNames.
	order int
	run   func(ctx context.Context, h *harness, rec *recorder, res *ScenarioResult) error
}

const (
	day  = 24 * time.Hour
	week = 7 * day
)

var scenarios = map[string]scenario{
	"cold-overview": {"The Overview's calls once, at once, right after the store opened: DuckDB's buffers are empty; the OS file cache is not dropped.", 0,
		func(ctx context.Context, h *harness, rec *recorder, _ *ScenarioResult) error {
			h.loadPage(ctx, rec, overviewPage(h.now, day))
			return nil
		}},
	"warm-overview": {"The Overview's calls at once, 10 times in a row, 24-hour range.", 1,
		func(ctx context.Context, h *harness, rec *recorder, _ *ScenarioResult) error {
			for range 10 {
				h.loadPage(ctx, rec, overviewPage(h.now, day))
			}
			return nil
		}},
	"overview-7d": {"The Overview's calls at once, 5 times in a row, 7-day range.", 2,
		func(ctx context.Context, h *harness, rec *recorder, _ *ScenarioResult) error {
			for range 5 {
				h.loadPage(ctx, rec, overviewPage(h.now, week))
			}
			return nil
		}},
	"tabs-1":   {"1 client cycling through the Overview and nine other pages, 24-hour range.", 3, tabs(1)},
	"tabs-4":   {"4 clients cycling through the pages at once, 24-hour range.", 4, tabs(4)},
	"tabs-16":  {"16 clients cycling through the pages at once, 24-hour range.", 5, tabs(16)},
	"agents-1": {"1 agent alternating a 32-series GetTimeSeriesBatch and ListMetricContributors, 24-hour range.", 6, agents(1)},
	"agents-4": {"4 agents at once.", 7, agents(4)},
	"agents-8": {"8 agents at once.", 8, agents(8)},
	"batch-vs-serial": {"One client: the 32-series batch, then the same 32 series as GetTimeSeries calls one after another; 5 rounds.", 9,
		func(ctx context.Context, h *harness, rec *recorder, _ *ScenarioResult) error {
			batch := agentCalls(h.now, day, h.streams, 0)[0]
			serial := serialSeries(h.now, day, h.streams)
			for range 5 {
				h.loadPage(ctx, rec, page{name: "batch", calls: []call{batch}})
				h.loadPage(ctx, rec, page{name: "serial", calls: []call{serial}})
			}
			return nil
		}},
	"writes-only": {"Epochs written every interval (10 s by default) with their checks, and the archive exporting, with no API calls.", 10,
		func(ctx context.Context, h *harness, rec *recorder, res *ScenarioResult) error {
			return h.background(ctx, res, nil)
		}},
	"mixed": {"4 tab clients and 2 agents while epochs are written every interval (10 s by default) with their checks and the archive exports.", 11,
		func(ctx context.Context, h *harness, rec *recorder, res *ScenarioResult) error {
			return h.background(ctx, res, func(ctx context.Context) {
				var wg sync.WaitGroup
				wg.Go(func() { _ = tabs(4)(ctx, h, rec, res) })
				wg.Go(func() { _ = agents(2)(ctx, h, rec, res) })
				wg.Wait()
			})
		}},
}

// ScenarioNames lists the scenarios in the order a full run takes them.
func ScenarioNames() []string {
	var out []string
	for n := range scenarios {
		out = append(out, n)
	}
	slices.SortFunc(out, func(a, b string) int { return scenarios[a].order - scenarios[b].order })
	return out
}

// tabs runs n clients, each loading the Overview and then the other pages
// in turn, client i starting at page i, until the scenario's duration
// ends.
func tabs(n int) func(context.Context, *harness, *recorder, *ScenarioResult) error {
	return func(ctx context.Context, h *harness, rec *recorder, _ *ScenarioResult) error {
		ctx, cancel := context.WithTimeout(ctx, h.opts.Duration)
		defer cancel()
		pages := append([]page{overviewPage(h.now, day)}, tabPages(h.now, day)...)
		var wg sync.WaitGroup
		for i := range n {
			wg.Go(func() {
				for k := i; ctx.Err() == nil; k++ {
					h.loadPage(ctx, rec, pages[k%len(pages)])
				}
			})
		}
		wg.Wait()
		return nil
	}
}

// agents runs n agents, each alternating its batch and its contributors
// call until the scenario's duration ends.
func agents(n int) func(context.Context, *harness, *recorder, *ScenarioResult) error {
	return func(ctx context.Context, h *harness, rec *recorder, _ *ScenarioResult) error {
		ctx, cancel := context.WithTimeout(ctx, h.opts.Duration)
		defer cancel()
		var wg sync.WaitGroup
		for i := range n {
			calls := agentCalls(h.now, day, h.streams, i)
			wg.Go(func() {
				for k := 0; ctx.Err() == nil; k++ {
					c := calls[k%len(calls)]
					h.loadPage(ctx, rec, page{name: "agent " + c.proc, calls: []call{c}})
				}
			})
		}
		wg.Wait()
		return nil
	}
}

// background writes epochs and exports the archive for the scenario's
// duration, running front (if any) at the same time.
func (h *harness) background(ctx context.Context, res *ScenarioResult, front func(context.Context)) error {
	ctx, cancel := context.WithTimeout(ctx, h.opts.Duration)
	defer cancel()
	dir, cleanup, err := scratchDir()
	if err != nil {
		return err
	}
	defer cleanup()
	w := &writer{h: h, interval: h.opts.interval()}
	var wg sync.WaitGroup
	wg.Go(func() { w.run(ctx) })
	var archived int
	var archiveTook []time.Duration
	var archiveErr error
	wg.Go(func() { archived, archiveTook, archiveErr = h.archiveLoop(ctx, dir) })
	if front != nil {
		front(ctx)
	}
	wg.Wait()
	res.Background = &Background{
		EpochInterval: w.interval, Epochs: len(w.writes), Missed: w.missed, Lost: w.lost,
		WriteP50: percentile(w.writes, 0.5), WriteP95: percentile(w.writes, 0.95), WriteMax: percentile(w.writes, 1),
		ChecksP50: percentile(w.checks, 0.5), ChecksMax: percentile(w.checks, 1),
		LagP50: percentile(w.lags, 0.5), LagMax: percentile(w.lags, 1),
		ArchivedHours: archived, ArchiveTickP50: percentile(archiveTook, 0.5), ArchiveTickMax: percentile(archiveTook, 1),
	}
	for _, e := range w.errs {
		res.Background.Errors = append(res.Background.Errors, e.Error())
	}
	if archiveErr != nil {
		res.Background.Errors = append(res.Background.Errors, "archive: "+archiveErr.Error())
	}
	return nil
}

// run runs one scenario and gathers its client and server numbers.
func (h *harness) run(ctx context.Context, name string) (ScenarioResult, error) {
	sc := scenarios[name]
	res := ScenarioResult{Name: name, Description: sc.description, LoadBefore: loadAverage()}
	before := snapshotTimings()
	mem := startMemSampler(h.st)
	waitsBefore := h.st.DB().Stats().WaitCount
	rec := &recorder{}
	start := time.Now()
	err := sc.run(ctx, h, rec, &res)
	res.Elapsed = time.Since(start)
	res.MaxHeapBytes = mem.end()
	res.MaxBudgetActive, res.MaxBudgetQueued, res.MaxConnsOpen, res.MaxConnsInUse = mem.maxActive, mem.maxQueued, mem.maxOpen, mem.maxInUse
	res.ConnWaits = h.st.DB().Stats().WaitCount - waitsBefore
	res.MaxRSSBytes = maxRSS()
	res.LoadAfter = loadAverage()
	res.summarize(rec, diffTimings(before, snapshotTimings()))
	return res, err
}

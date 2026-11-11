package perf

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/archive"
	"github.com/shadone/nats-lens/internal/checks"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/timing"
	"github.com/shadone/nats-lens/internal/webui"
)

// SystemID is the ID the harness serves its store under.
const SystemID = "perf"

// LoadOptions selects the store and the scenarios of one load run.
type LoadOptions struct {
	Profile Profile
	Path    string
	// Scenarios to run, in order; empty runs ScenarioNames().
	Scenarios []string
	// Duration of each concurrent scenario.
	Duration time.Duration
	Log      *slog.Logger
}

// harness is one opened store served over loopback HTTP.
type harness struct {
	opts    LoadOptions
	st      *store.Store
	set     *checks.Set
	client  natslensv1connect.LensServiceClient
	streams []*v1.EntityRef
	// last is the start of the newest stored epoch, in Unix microseconds;
	// the provider's clock stands 5 seconds after it.
	last     atomic.Int64
	epoch    atomic.Int64
	log      *slog.Logger
	openTook time.Duration
	// planJSON is where Explain writes the next JSON plan.
	planJSON string
}

func (h *harness) now() time.Time { return time.UnixMicro(h.last.Load()).Add(5 * time.Second).UTC() }

// Load opens the store, serves it on a loopback port and runs the
// scenarios. The store gains the epochs, findings and archive records
// the scenarios that write produce.
func Load(ctx context.Context, o LoadOptions) (*Report, error) {
	if len(o.Scenarios) == 0 {
		o.Scenarios = ScenarioNames()
	}
	for _, n := range o.Scenarios {
		if _, ok := scenarios[n]; !ok {
			return nil, fmt.Errorf("unknown scenario %q", n)
		}
	}
	h, closeAll, err := openHarness(ctx, o)
	if err != nil {
		return nil, err
	}
	defer closeAll()
	rep := &Report{Env: environment(ctx, h.st, o, h.openTook)}
	for _, name := range o.Scenarios {
		if ctx.Err() != nil {
			return rep, ctx.Err()
		}
		if name != "cold-overview" && h.streams == nil {
			if err := h.loadStreams(ctx); err != nil {
				return rep, err
			}
		}
		o.Log.Info("scenario", "name", name)
		res, err := h.run(ctx, name)
		if err != nil {
			return rep, fmt.Errorf("%s: %w", name, err)
		}
		rep.Results = append(rep.Results, res)
	}
	return rep, nil
}

// openHarness opens the store and serves it over loopback HTTP; the
// returned func stops the server and closes the store.
func openHarness(ctx context.Context, o LoadOptions) (*harness, func(), error) {
	opened := time.Now()
	st, err := store.OpenPath(ctx, o.Path)
	if err != nil {
		return nil, nil, err
	}
	st.SetSystem(SystemID)
	openTook := time.Since(opened)
	set, err := checks.Load(checksConfig())
	if err != nil {
		_ = st.Close()
		return nil, nil, err
	}
	h := &harness{opts: o, st: st, set: set, log: o.Log, openTook: openTook}
	var last time.Time
	var epoch int64
	if err := st.DB().QueryRowContext(ctx, `SELECT epoch, started_at FROM epochs ORDER BY epoch DESC LIMIT 1`).Scan(&epoch, &last); err != nil {
		_ = st.Close()
		return nil, nil, fmt.Errorf("newest epoch: %w", err)
	}
	h.last.Store(last.UnixMicro())
	h.epoch.Store(epoch)
	p := &storeprov.Provider{St: st, Interval: o.Profile.Interval, Retention: o.Profile.History + 24*time.Hour, Live: true,
		Checks: set, Clock: h.now}
	handler := &api.Handler{Systems: api.Systems{{Ref: &v1.SystemRef{Id: SystemID, Name: "Perf"}, Provider: p}},
		Log: slog.New(slog.DiscardHandler)}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = st.Close()
		return nil, nil, err
	}
	srv := &http.Server{Handler: api.NewMux(handler, webui.FS()), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	base := "http://" + ln.Addr().String() + api.PathPrefix
	hc := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 64}}
	h.client = natslensv1connect.NewLensServiceClient(hc, base, connect.WithSendMaxBytes(api.MaxMessageBytes), connect.WithReadMaxBytes(api.MaxMessageBytes))
	return h, func() { _ = srv.Close(); _ = st.Close() }, nil
}

// loadStreams reads the streams an agent's series ask for.
func (h *harness) loadStreams(ctx context.Context) error {
	res, err := h.client.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Range: liveRange(h.now(), time.Hour), Limit: 256}))
	if err != nil {
		return fmt.Errorf("list streams: %w", err)
	}
	for _, s := range res.Msg.GetStreams() {
		h.streams = append(h.streams, s.GetEntity())
	}
	if len(h.streams) == 0 {
		return errors.New("the store lists no streams")
	}
	return nil
}

// sample is one finished call.
type sample struct {
	proc string
	took time.Duration
	err  error
}

// recorder collects samples from concurrent clients.
type recorder struct {
	mu      sync.Mutex
	samples []sample
	pages   map[string][]time.Duration
}

func (r *recorder) add(s sample) {
	r.mu.Lock()
	r.samples = append(r.samples, s)
	r.mu.Unlock()
}

func (r *recorder) page(name string, d time.Duration) {
	r.mu.Lock()
	if r.pages == nil {
		r.pages = map[string][]time.Duration{}
	}
	r.pages[name] = append(r.pages[name], d)
	r.mu.Unlock()
}

// loadPage starts a page's calls at once and waits for all of them, as
// the UI does; it records each call and the page's time to its last
// answer.
func (h *harness) loadPage(ctx context.Context, rec *recorder, p page) {
	start := time.Now()
	var wg sync.WaitGroup
	for _, c := range p.calls {
		wg.Go(func() {
			t := time.Now()
			err := c.do(ctx, h.client)
			if ctx.Err() == nil || err == nil {
				rec.add(sample{proc: c.proc, took: time.Since(t), err: err})
			}
		})
	}
	wg.Wait()
	if ctx.Err() == nil {
		rec.page(p.name, time.Since(start))
	}
}

// memSampler records the largest Go heap in use while it runs.
type memSampler struct {
	stop    chan struct{}
	done    chan struct{}
	maxHeap atomic.Uint64
}

func startMemSampler() *memSampler {
	m := &memSampler{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(m.done)
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for {
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			if ms.HeapInuse > m.maxHeap.Load() {
				m.maxHeap.Store(ms.HeapInuse)
			}
			select {
			case <-m.stop:
				return
			case <-t.C:
			}
		}
	}()
	return m
}

func (m *memSampler) end() uint64 {
	close(m.stop)
	<-m.done
	return m.maxHeap.Load()
}

// maxRSS is the process's peak resident set, Go heap and DuckDB together.
func maxRSS() int64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	if runtime.GOOS == "darwin" {
		return ru.Maxrss
	}
	return ru.Maxrss * 1024
}

// writer takes epochs while a scenario runs: every interval it writes the
// next synthetic epoch through Store.WriteSnapshotWithin, then runs the
// checks, as the runner does; it records how late each epoch was stored.
type writer struct {
	h        *harness
	interval time.Duration
	lags     []time.Duration
	writes   []time.Duration
	checks   []time.Duration
	missed   int
	errs     []error
}

func (w *writer) run(ctx context.Context) {
	eng := &checks.Engine{Set: w.h.set, Log: slog.New(slog.DiscardHandler), Interval: w.interval, Budget: w.interval / 2}
	tick := time.NewTicker(w.interval)
	defer tick.Stop()
	due := time.Now()
	for {
		n := int(w.h.epoch.Load()) + 1
		at := time.UnixMicro(w.h.last.Load()).Add(w.h.opts.Profile.Interval).UTC()
		t := time.Now()
		wctx := timing.WithSource(context.WithoutCancel(ctx), timing.SourceEpoch)
		epoch, err := w.h.st.WriteSnapshotWithin(wctx, w.h.opts.Profile.Snapshot(n, at), w.interval)
		w.writes = append(w.writes, time.Since(t))
		if err != nil {
			w.errs = append(w.errs, err)
		} else {
			w.h.epoch.Store(epoch)
			w.h.last.Store(at.UnixMicro())
			c := time.Now()
			if _, err := eng.Run(timing.WithSource(context.WithoutCancel(ctx), timing.SourceChecks), w.h.st, epoch); err != nil {
				w.errs = append(w.errs, err)
			}
			w.checks = append(w.checks, time.Since(c))
		}
		w.lags = append(w.lags, time.Since(due))
		if time.Since(due) > w.interval {
			w.missed++
		}
		select {
		case <-ctx.Done():
			return
		case due = <-tick.C:
		}
	}
}

// archiver exports the store's pending hours, oldest first, two at a time,
// into a scratch directory, until ctx ends.
func (h *harness) archiveLoop(ctx context.Context, dir string) (exported int, took []time.Duration, err error) {
	a := &archive.Archiver{Store: h.st, Dest: archive.LocalDir{Dir: dir}, Retention: h.opts.Profile.History + time.Hour,
		Grace: time.Minute, MaxHoursPerTick: 2, Log: slog.New(slog.DiscardHandler)}
	for ctx.Err() == nil {
		t := time.Now()
		n, e := a.Export(ctx, h.now())
		if e != nil {
			if ctx.Err() != nil {
				return exported, took, nil
			}
			return exported, took, e
		}
		exported += n
		took = append(took, time.Since(t))
		if n == 0 {
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
		}
	}
	return exported, took, nil
}

func scratchDir() (string, func(), error) {
	d, err := os.MkdirTemp("", "natslens-perf-archive-")
	if err != nil {
		return "", nil, err
	}
	return d, func() { _ = os.RemoveAll(d) }, nil
}

// percentile returns the q-quantile (0..1) of ds by nearest rank.
func percentile(ds []time.Duration, q float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	s := slices.Clone(ds)
	slices.Sort(s)
	i := int(q*float64(len(s))+0.5) - 1
	return s[min(max(i, 0), len(s)-1)]
}

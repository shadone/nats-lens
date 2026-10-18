package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"path"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
)

// PathPrefix is where the API is mounted: a client's base URL is
// http://host/api.
const PathPrefix = "/api"

// DefaultListen is the address the API binds to without -listen. It is
// localhost because the tool has no authentication of its own.
const DefaultListen = "127.0.0.1:8080"

// MaxMessageBytes bounds a request and a response message, after
// decompression.
const MaxMessageBytes = 1 << 20

// Server limits.
const (
	readTimeout       = 60 * time.Second
	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 120 * time.Second
	maxHeaderBytes    = 64 << 10
)

// DefaultMetricsTimeout bounds each system's part of a /metrics scrape:
// below Prometheus's usual 10-second scrape_timeout, so that one hung
// system does not make Prometheus give up on the whole target.
const DefaultMetricsTimeout = 8 * time.Second

// metricsTimeout is the timeout of a Handler without MetricsTimeout; a
// variable so that tests can shorten it.
var metricsTimeout = DefaultMetricsTimeout

// shutdownTimeout is how long a shutdown waits for calls to finish before
// it closes their connections; a variable so that tests can shorten it.
var shutdownTimeout = 5 * time.Second

// NewMux returns the HTTP handler of the tool: the API under /api/ (the
// Connect, gRPC and gRPC-Web protocols), /healthz, and the UI files at /,
// every response with the security headers.
func NewMux(h *Handler, ui fs.FS) http.Handler {
	mux := http.NewServeMux()
	p, svc := natslensv1connect.NewLensServiceHandler(h,
		connect.WithReadMaxBytes(MaxMessageBytes),
		connect.WithSendMaxBytes(MaxMessageBytes),
		connect.WithRecover(func(_ context.Context, spec connect.Spec, _ http.Header, r any) error {
			if h.Log != nil {
				h.Log.Error("api call panicked", "procedure", spec.Procedure, "panic", r, "stack", string(debug.Stack()))
			}
			return connect.NewError(connect.CodeInternal, errors.New("internal error; see the server log"))
		}),
	)
	mux.Handle(PathPrefix+p, http.StripPrefix(PathPrefix, svc))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	if metricsWriters(h.Systems) {
		mux.HandleFunc("GET /metrics", metricsHandler(h))
	}
	mux.Handle(PathPrefix+"/", http.NotFoundHandler())
	mux.Handle("/", uiHandler(ui))
	return withSecurityHeaders(contentSecurityPolicy(ui), mux)
}

// MetricsWriter is a provider that serves Prometheus metrics at /metrics.
// It declares its families and adds its samples to m; the handler labels
// them with the system. WriteMetrics must return when ctx ends: the
// handler stops waiting then, but runs no second WriteMetrics for the
// system until the first has returned.
type MetricsWriter interface {
	WriteMetrics(ctx context.Context, m *Exposition) error
}

// metricsWriters reports whether any system's provider writes metrics.
func metricsWriters(systems Systems) bool {
	for _, s := range systems {
		if _, ok := s.Provider.(MetricsWriter); ok {
			return true
		}
	}
	return false
}

// metricsHandler reads the metrics of every system that writes them, in
// parallel, each within the handler's metrics timeout and into an
// exposition of its own,
// and merges a system's samples only when it succeeded, so that a failure
// halfway leaves no partial families. nats_lens_system_scrape_success
// says which systems were read. The answer is 200 while any system
// succeeds and 500 when all fail.
func metricsHandler(h *Handler) http.HandlerFunc {
	var writers []System
	for _, s := range h.Systems {
		if _, ok := s.Provider.(MetricsWriter); ok {
			writers = append(writers, s)
		}
	}
	// running[i] is set while a WriteMetrics of writers[i] runs, which may
	// outlast the scrape that started it.
	running := make([]atomic.Bool, len(writers))
	return func(w http.ResponseWriter, r *http.Request) {
		timeout := h.MetricsTimeout
		if timeout <= 0 {
			timeout = metricsTimeout
		}
		type result struct {
			id  string
			exp *Exposition
			err error
		}
		results := make([]result, len(writers))
		var wg sync.WaitGroup
		for i, s := range writers {
			wg.Go(func() {
				id := s.Ref.GetId()
				ctx, cancel := context.WithTimeout(r.Context(), timeout)
				defer cancel()
				exp, err := NewExposition(id)
				if err == nil {
					err = writeMetrics(ctx, s.Provider.(MetricsWriter), exp, &running[i])
				}
				results[i] = result{id: id, exp: exp, err: err}
			})
		}
		wg.Wait()
		var out families
		out.declare("nats_lens_system_scrape_success", "gauge", "1 when the system's metrics were read for this scrape, 0 when they failed.")
		var merged []*families
		ok := 0
		for _, res := range results {
			v := 0.0
			if res.err == nil {
				v = 1
				ok++
				merged = append(merged, &res.exp.fams)
			} else if h.Log != nil {
				h.Log.Error("metrics failed", "system", res.id, "err", res.err)
			}
			if res.id != "" {
				status, _ := NewExposition(res.id)
				status.Sample("nats_lens_system_scrape_success", v)
				out.merge(&status.fams)
			}
		}
		if len(writers) > 0 && ok == 0 {
			http.Error(w, "metrics unavailable; see the server log", http.StatusInternalServerError)
			return
		}
		for _, m := range merged {
			out.merge(m)
		}
		var buf bytes.Buffer
		if _, err := out.writeTo(&buf); err != nil {
			if h.Log != nil {
				h.Log.Error("metrics failed", "err", err)
			}
			http.Error(w, "metrics unavailable; see the server log", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write(buf.Bytes())
	}
}

// writeMetrics runs one system's WriteMetrics, returning when ctx ends
// even if the provider does not, so that a hung system cannot hold the
// scrape past its timeout. running is held from the start of the call
// until the provider returns: while it is held, the system fails at once
// and no second call starts. A panic in the provider fails the system.
func writeMetrics(ctx context.Context, mw MetricsWriter, exp *Exposition, running *atomic.Bool) error {
	if !running.CompareAndSwap(false, true) {
		return errors.New("the previous metrics call of this system is still running")
	}
	done := make(chan error, 1)
	scratch, _ := NewExposition(exp.system)
	go func() {
		defer running.Store(false)
		defer func() {
			if p := recover(); p != nil {
				done <- fmt.Errorf("metrics panicked: %v\n%s", p, debug.Stack())
			}
		}()
		done <- mw.WriteMetrics(ctx, scratch)
	}()
	select {
	case err := <-done:
		if err == nil {
			exp.fams = scratch.fams
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// uiHandler serves the UI files. A GET for a path that names no file and
// whose last element has no dot gets index.html, so that the UI's own
// routes load the app; a missing asset is a 404.
func uiHandler(ui fs.FS) http.Handler {
	files := http.FileServerFS(ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			files.ServeHTTP(w, r)
			return
		}
		if _, err := fs.Stat(ui, name); err == nil {
			files.ServeHTTP(w, r)
			return
		}
		if strings.Contains(path.Base(name), ".") {
			http.NotFound(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		files.ServeHTTP(w, r2)
	})
}

// DefaultAllowedHosts is the Host allowlist for a listen address: the
// loopback names when the address is a loopback one, and none (any host)
// otherwise.
func DefaultAllowedHosts(listen string) []string {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return nil
	}
	if host == "localhost" {
		return []string{"localhost", "127.0.0.1", "::1"}
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return []string{"localhost", "127.0.0.1", "::1"}
	}
	return nil
}

// HostExemptPaths are the paths AllowHosts leaves open for a listen
// address. On a listener that is not loopback, Prometheus scrapes
// /metrics by the pod's address, so /metrics is exempt there; it is read
// only but names entities, so such a listener must not be reachable from
// outside the cluster. On loopback the allowlist keeps guarding /metrics
// against DNS rebinding.
func HostExemptPaths(listen string) []string {
	if IsLoopbackListen(listen) {
		return nil
	}
	return []string{"/metrics"}
}

// IsLoopbackListen reports whether a listen address is a loopback one.
func IsLoopbackListen(listen string) bool { return len(DefaultAllowedHosts(listen)) > 0 }

// AllowHosts rejects requests whose Host is not in hosts, which keeps a
// web page on another origin from reaching a localhost API through DNS
// rebinding. Empty hosts allows every host; /healthz and the exempt paths
// are always allowed.
func AllowHosts(hosts []string, next http.Handler, exempt ...string) http.Handler {
	if len(hosts) == 0 {
		return next
	}
	ok := map[string]bool{}
	for _, h := range hosts {
		ok[strings.ToLower(h)] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.ToLower(strings.Trim(host, "[]"))
		if !ok[host] && r.URL.Path != "/healthz" && !slices.Contains(exempt, r.URL.Path) {
			http.Error(w, "host not allowed", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Serve listens on addr and serves handler until ctx is done.
func Serve(ctx context.Context, addr string, handler http.Handler, log *slog.Logger) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return ServeListener(ctx, ln, handler, log)
}

// ServeListener serves handler on ln until ctx is done, with HTTP/2
// without TLS (h2c) enabled so that gRPC clients work as well as
// browsers. On shutdown it waits shutdownTimeout for calls to finish, then
// closes the connections; it returns only after every handler returned,
// so that the caller may close what the handlers use.
func ServeListener(ctx context.Context, ln net.Listener, handler http.Handler, log *slog.Logger) error {
	calls := &callTracker{}
	srv := newHTTPServer(calls.wrap(handler), log)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	log.Info("serving", "addr", ln.Addr().String(), "api", PathPrefix+"/")
	var err error
	select {
	case err = <-errc:
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		if serr := srv.Shutdown(sctx); serr != nil {
			log.Warn("api shutdown timed out; closing connections", "err", serr)
			_ = srv.Close()
		}
		cancel()
		err = <-errc
	}
	_ = srv.Close()
	calls.closeAndWait()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// newHTTPServer builds the server with its limits. WriteTimeout leaves the
// longest call, GetTimeSeriesBatch, its BatchTimeout and 15 seconds to
// write the response.
func newHTTPServer(handler http.Handler, log *slog.Logger) *http.Server {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	return &http.Server{
		Handler:           handler,
		Protocols:         &protocols,
		ReadTimeout:       readTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      BatchTimeout + 15*time.Second,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
}

// callTracker counts running calls. Once closed it admits no new call,
// so the wait for the running ones cannot race a call that starts during
// it, which a WaitGroup reused across shutdown would allow.
type callTracker struct {
	mu     sync.Mutex
	n      int
	closed bool
	idle   chan struct{} // closed when closed is set and n is zero
}

// enter admits a call; false after closeAndWait began.
func (c *callTracker) enter() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.n++
	return true
}

func (c *callTracker) leave() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n--
	if c.closed && c.n == 0 {
		close(c.idle)
	}
}

// closeAndWait stops admitting calls and waits until the running ones
// have returned.
func (c *callTracker) closeAndWait() {
	c.mu.Lock()
	c.closed = true
	c.idle = make(chan struct{})
	if c.n == 0 {
		close(c.idle)
	}
	idle := c.idle
	c.mu.Unlock()
	<-idle
}

func (c *callTracker) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !c.enter() {
			http.Error(w, "shutting down", http.StatusServiceUnavailable)
			return
		}
		defer c.leave()
		next.ServeHTTP(w, r)
	})
}

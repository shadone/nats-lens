package api

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"path"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
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
// them with the system.
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

// metricsHandler collects the metrics of every system that writes them,
// each sample labelled with its system, and writes them into a buffer
// first, so that a failed read answers 500 instead of a cut exposition.
func metricsHandler(h *Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), QueryTimeout)
		defer cancel()
		m := NewExposition()
		for _, s := range h.Systems {
			mw, ok := s.Provider.(MetricsWriter)
			if !ok {
				continue
			}
			m.SetSystem(s.Ref.GetId())
			if err := mw.WriteMetrics(ctx, m); err != nil {
				if h.Log != nil {
					h.Log.Error("metrics failed", "system", s.Ref.GetId(), "err", err)
				}
				http.Error(w, "metrics unavailable; see the server log", http.StatusInternalServerError)
				return
			}
		}
		var buf bytes.Buffer
		if _, err := m.WriteTo(&buf); err != nil {
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

// newHTTPServer builds the server with its limits. WriteTimeout leaves a
// call its QueryTimeout and 15 seconds to write the response.
func newHTTPServer(handler http.Handler, log *slog.Logger) *http.Server {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	return &http.Server{
		Handler:           handler,
		Protocols:         &protocols,
		ReadTimeout:       readTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      QueryTimeout + 15*time.Second,
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

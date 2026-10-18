package api

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"
)

func TestDefaultAllowedHosts(t *testing.T) {
	for listen, local := range map[string]bool{
		"127.0.0.1:8080": true, "localhost:8080": true, "[::1]:8080": true,
		":8080": false, "0.0.0.0:8080": false, "10.0.0.5:8080": false, "bad": false,
	} {
		if got := DefaultAllowedHosts(listen); (len(got) > 0) != local {
			t.Errorf("%s: %v", listen, got)
		}
	}
}

func TestAllowHosts(t *testing.T) {
	h := AllowHosts([]string{"localhost", "127.0.0.1", "::1"}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	for host, want := range map[string]int{
		"localhost:8080": 200, "LOCALHOST": 200, "127.0.0.1:8080": 200, "[::1]:8080": 200,
		"evil.example:8080": http.StatusMisdirectedRequest, "127.0.0.1.nip.io": http.StatusMisdirectedRequest,
	} {
		r := httptest.NewRequest("GET", "/api/x", nil)
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("host %s: %d, want %d", host, w.Code, want)
		}
	}
	r := httptest.NewRequest("GET", "/healthz", nil)
	r.Host = "10.0.0.7:8080"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("healthz from a probe: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/api/x", nil)
	r.Host = "evil.example"
	w = httptest.NewRecorder()
	AllowHosts(nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})).ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("an empty allowlist rejected a host: %d", w.Code)
	}
}

func TestUIFallback(t *testing.T) {
	ui := fstest.MapFS{"index.html": {Data: []byte("app")}, "assets/app.js": {Data: []byte("js")}}
	h := uiHandler(ui)
	for _, c := range []struct {
		method, path string
		code         int
		body         string
	}{
		{"GET", "/", 200, "app"},
		{"GET", "/streams/order-jobs", 200, "app"},
		{"GET", "/assets/app.js", 200, "js"},
		{"GET", "/assets/missing.js", 404, ""},
		{"GET", "/favicon.ico", 404, ""},
		{"HEAD", "/servers", 200, ""},
		{"POST", "/servers", 405, ""},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
		if w.Code != c.code || (c.body != "" && w.Body.String() != c.body) {
			t.Errorf("%s %s: %d %q", c.method, c.path, w.Code, w.Body.String())
		}
	}
}

// ServeListener returns only after every handler returned, and closes
// connections whose calls outlast the shutdown timeout.
func TestServeListenerWaitsForHandlers(t *testing.T) {
	old := shutdownTimeout
	shutdownTimeout = 100 * time.Millisecond
	t.Cleanup(func() { shutdownTimeout = old })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started, released := make(chan struct{}), make(chan struct{})
	var handlerDone time.Time
	h := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done() // the call outlasts the shutdown timeout
		time.Sleep(50 * time.Millisecond)
		handlerDone = time.Now()
		close(released)
	})
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ServeListener(ctx, ln, h, slog.New(slog.DiscardHandler)) }()
	go func() {
		res, err := http.Get("http://" + ln.Addr().String() + "/slow")
		if err == nil {
			_, _ = io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}
	}()
	<-started
	stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ServeListener did not return after the shutdown timeout")
	}
	select {
	case <-released:
	default:
		t.Fatal("ServeListener returned while a handler was running")
	}
	if handlerDone.IsZero() {
		t.Fatal("handler did not finish")
	}
}

func TestCallTrackerRefusesCallsAfterClose(t *testing.T) {
	var c callTracker
	if !c.enter() {
		t.Fatal("a call before close was refused")
	}
	done := make(chan struct{})
	go func() { c.closeAndWait(); close(done) }()
	select {
	case <-done:
		t.Fatal("closeAndWait returned while a call ran")
	case <-time.After(50 * time.Millisecond):
	}
	for !func() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.closed }() {
		time.Sleep(time.Millisecond)
	}
	if c.enter() {
		t.Fatal("a call was admitted after close")
	}
	c.leave()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("closeAndWait did not return after the last call")
	}
	var idle callTracker
	idle.closeAndWait() // returns at once without calls
}

func TestServerLimits(t *testing.T) {
	srv := newHTTPServer(http.NotFoundHandler(), slog.New(slog.DiscardHandler))
	if srv.WriteTimeout != BatchTimeout+15*time.Second || srv.ReadTimeout == 0 || srv.IdleTimeout == 0 || srv.MaxHeaderBytes != 64<<10 {
		t.Fatalf("limits: write %v, read %v, idle %v, header %d", srv.WriteTimeout, srv.ReadTimeout, srv.IdleTimeout, srv.MaxHeaderBytes)
	}
}

func TestAllowHostsExemptPaths(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})
	for _, c := range []struct {
		exempt []string
		want   int
	}{{nil, http.StatusMisdirectedRequest}, {[]string{"/metrics"}, 200}} {
		r := httptest.NewRequest("GET", "/metrics", nil)
		r.Host = "10.0.0.7:8080"
		w := httptest.NewRecorder()
		AllowHosts([]string{"localhost"}, next, c.exempt...).ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("exempt %v: %d, want %d", c.exempt, w.Code, c.want)
		}
	}
	if !IsLoopbackListen("127.0.0.1:8080") || IsLoopbackListen(":8080") {
		t.Error("IsLoopbackListen")
	}
}

// /metrics is exempt from the allowlist only on a listener that is not
// loopback, where Prometheus scrapes by the pod's address; on loopback the
// allowlist guards it against DNS rebinding like the API.
func TestMetricsExemption(t *testing.T) {
	if got := HostExemptPaths("127.0.0.1:8080"); len(got) != 0 {
		t.Errorf("loopback: %v", got)
	}
	if got := HostExemptPaths("0.0.0.0:8080"); len(got) != 1 || got[0] != "/metrics" {
		t.Errorf("all interfaces: %v", got)
	}
}

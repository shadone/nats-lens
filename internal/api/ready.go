package api

import "net/http"

// ReadyPath answers whether the process is ready for traffic.
const ReadyPath = "/readyz"

// WithReady serves ReadyPath in front of next: 200 once ready reports
// true, 503 before; a nil ready is always ready. natslens run reports
// ready once its first epoch is stored, so that a new pod receives no
// traffic while it rebuilds its database or before it reached NATS.
func WithReady(next http.Handler, ready func() bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ReadyPath {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if ready != nil && !ready() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("waiting for the first epoch\n"))
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})
}

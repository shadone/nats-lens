package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// The server route, not a cooperative client, must reject every write.
func TestInvestigationMethodBoundary(t *testing.T) {
	h := NewMux(&Handler{}, fstest.MapFS{"index.html": {Data: []byte("app")}})
	for _, method := range []string{"MuteFinding", "UnmuteFinding", "AcknowledgeFinding", "EndAcknowledgement", "ExecuteSQL", "GetFutureMethod"} {
		for _, verb := range []string{"POST", "GET"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(verb, "/api/read-only/natslens.v1.LensService/"+method, strings.NewReader("{}")))
			if w.Code != http.StatusForbidden {
				t.Errorf("%s %s: got %d, want 403", verb, method, w.Code)
			}
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/read-only/natslens.v1.LensService/ListMetrics", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "metrics") {
		t.Fatalf("read: %d %s", w.Code, w.Body.String())
	}
}

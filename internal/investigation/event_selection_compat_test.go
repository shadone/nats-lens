package investigation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExactEventReadsRejectIgnoredSelectionFields(t *testing.T) {
	for _, tc := range []struct{ name, method, request, response string }{
		{"legacy selection", "ListEvents", `{"selectedIds":["1"],"pageSize":1}`, `{"events":[{"id":"2"}],"totalCount":1}`},
		{"legacy detail boundary", "GetEvent", `{"id":"1","eventReadId":"0"}`, `{"event":{"id":"1"}}`},
		{"legacy Timeline boundary", "GetStatusTimeline", `{"eventReadId":"0"}`, `{}`},
		{"wrong selection despite echo", "ListEvents", `{"selectedIds":["1"],"eventReadId":"3"}`, `{"selectedIds":["1"],"eventReadId":"3","events":[{"id":"2"}]}`},
		{"wrong event despite echo", "GetEvent", `{"id":"1","eventReadId":"3"}`, `{"eventReadId":"3","event":{"id":"2"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			remote, err := NewRemote(Profile{Deployment: server.URL, Endpoint: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			data, err := remote.Query(context.Background(), tc.method, []byte(tc.request))
			if err == nil || !strings.Contains(err.Error(), "event") {
				t.Fatalf("unacknowledged/broader read accepted: %s %v", data, err)
			}
		})
	}
}

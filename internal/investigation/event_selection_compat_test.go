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
		{"legacy batch", "GetEventBatch", `{"ids":["1"],"eventReadId":"3"}`, `{"events":[{"id":"2"}],"totalCount":1}`},
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

func TestExactEventReadsAcceptAcknowledgedSelection(t *testing.T) {
	for _, tc := range []struct{ method, request, response string }{
		{"ListEvents", `{"selectedIds":["9007199254740993"],"eventReadId":"9007199254740995"}`, `{"selectedIds":["9007199254740993"],"eventReadId":"9007199254740995","events":[{"id":"9007199254740993"}],"totalCount":1}`},
		{"GetEventBatch", `{"ids":["1"],"eventReadId":"3"}`, `{"selectedIds":["1"],"eventReadId":"3","events":[],"totalCount":0}`},
		{"ListEvents", `{"selectedIds":["1"]}`, `{"selectedIds":["1"],"events":[],"totalCount":0}`},
		{"ListEvents", `{"eventReadId":"0"}`, `{"eventReadId":"0","events":[],"totalCount":0}`},
		{"GetStatusTimeline", `{"eventReadId":"0"}`, `{"eventReadId":"0"}`},
		{"GetEvent", `{"id":"1","eventReadId":"3"}`, `{"eventReadId":"3","event":{"id":"1"}}`},
		{"ListEvents", `{}`, `{"events":[{"id":"1"}],"totalCount":1}`},
	} {
		t.Run(tc.method, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			remote, err := NewRemote(Profile{Deployment: server.URL, Endpoint: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = remote.Query(context.Background(), tc.method, []byte(tc.request)); err != nil {
				t.Fatalf("valid exact/legacy read rejected: %v", err)
			}
		})
	}
}

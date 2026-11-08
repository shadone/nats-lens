package investigation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/fake"
	inv "github.com/shadone/nats-lens/internal/investigation"
)

func TestPageBudgetReachesEntityRPCAndResponse(t *testing.T) {
	for _, method := range []string{"ListStreams", "ListConsumers", "ListMetricContributors"} {
		t.Run(method, func(t *testing.T) {
			var requests []map[string]json.RawMessage
			mux := api.NewMux(&api.Handler{Systems: fake.Demo(false)}, fstest.MapFS{"index.html": {Data: []byte("fixture")}})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/"+method) {
					var body bytes.Buffer
					body.ReadFrom(r.Body)
					var req map[string]json.RawMessage
					json.Unmarshal(body.Bytes(), &req)
					requests = append(requests, req)
					r.Body = io.NopCloser(bytes.NewReader(body.Bytes()))
				}
				mux.ServeHTTP(w, r)
			}))
			defer srv.Close()
			dir := t.TempDir()
			profile := filepath.Join(dir, "profiles.json")
			b, _ := json.Marshal(map[string]inv.Profile{"fixture": {Deployment: srv.URL, Endpoint: srv.URL}})
			os.WriteFile(profile, b, 0600)
			var out bytes.Buffer
			args := []string{"query", method, "-profiles", profile, "-profile", "fixture", "-system", "example-prod", "-page-size", "1"}
			if method == "ListMetricContributors" {
				request := filepath.Join(dir, "request.json")
				if err := os.WriteFile(request, []byte(`{"metric":"server.cpu_percent","ranking":"METRIC_RANKING_MAXIMUM","descending":true}`), 0600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "-request", request)
			}
			if err := inv.Run(context.Background(), args, strings.NewReader(""), &out, &out); err != nil {
				t.Fatal(err, out.String())
			}
			var result struct {
				Request  map[string]json.RawMessage `json:"request"`
				Response map[string]json.RawMessage `json:"response"`
			}
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if len(requests) != 1 || string(requests[0]["limit"]) != "1" || string(result.Request["limit"]) != "1" {
				t.Fatalf("page budget not sent to RPC: %v / %v", result.Request, requests)
			}
			key := "streams"
			if method == "ListConsumers" {
				key = "consumers"
			}
			if method == "ListMetricContributors" {
				key = "contributors"
			}
			var rows []json.RawMessage
			json.Unmarshal(result.Response[key], &rows)
			if len(rows) != 1 {
				t.Fatalf("requested one row, retrieved %d", len(rows))
			}
			var token string
			json.Unmarshal(result.Response["nextPageToken"], &token)
			if token == "" {
				t.Fatal("missing explicit continuation")
			}
			if (method != "ListMetricContributors" && string(result.Response["epoch"]) == "") || (method == "ListMetricContributors" && string(result.Response["readAt"]) == "") || string(result.Response["range"]) == "" {
				t.Fatal("pinned list metadata lost")
			}
		})
	}
}

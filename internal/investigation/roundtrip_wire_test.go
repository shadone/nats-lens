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
	"sync"
	"testing"
	"testing/fstest"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/fake"
	inv "github.com/shadone/nats-lens/internal/investigation"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestEmittedContextsReachRPCUnchanged(t *testing.T) {
	var mu sync.Mutex
	wire := map[string][]byte{}
	mux := api.NewMux(&api.Handler{Systems: fake.Demo(false)}, fstest.MapFS{"index.html": {Data: []byte("fixture")}})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		wire[r.URL.Path] = bytes.Clone(b)
		mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(b))
		mux.ServeHTTP(w, r)
	}))
	defer srv.Close()
	dir := t.TempDir()
	profile := filepath.Join(dir, "profiles.json")
	b, _ := json.Marshal(map[string]inv.Profile{"fixture": {Deployment: srv.URL, Endpoint: srv.URL}})
	os.WriteFile(profile, b, 0600)
	type result struct {
		Context  json.RawMessage
		Request  json.RawMessage
		Response map[string]json.RawMessage
		Links    []inv.EvidenceLink
	}
	query := func(method string, args ...string) result {
		t.Helper()
		var out bytes.Buffer
		all := append([]string{"query", method, "-profiles", profile, "-profile", "fixture"}, args...)
		if err := inv.Run(context.Background(), all, strings.NewReader(""), &out, &out); err != nil {
			t.Fatal(err, out.String())
		}
		var r result
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		sent := bytes.Clone(wire[inv.Path+"/"+inv.Service+"/"+method])
		mu.Unlock()
		if !json.Valid(sent) {
			t.Fatalf("no read-only wire request: %s", sent)
		}
		a := dynamicpb.NewMessage(inv.Method(method).Input())
		z := dynamicpb.NewMessage(inv.Method(method).Input())
		protojson.Unmarshal(sent, a)
		protojson.Unmarshal(r.Request, z)
		if !proto.Equal(a, z) {
			t.Fatal("envelope differs from outgoing RPC")
		}
		return r
	}
	for _, tc := range []struct{ method, path, request string }{
		{"ListConsumers", "/consumers?account=OTHER&stream=APP/order-jobs", `{"include_ephemeral":false,"limit":1}`},
		{"ListEvents", "/events?kind=%20finding.resolved%20&show=all", `{}`},
		{"GetSubjects", "/subjects", `{"filter":"SUBJECT_FILTER_WIDE"}`},
		{"GetPlacement", "/placement", `{"filter":"PLACEMENT_FILTER_MISSING"}`},
		{"GetStatusTimeline", "/", `{"includeOk":true,"bucket":"60s"}`},
		{"GetTimeSeries", "/servers/nats-0", `{"metric":"server.cpu_percent","maxPoints":7}`},
	} {
		t.Run(tc.method, func(t *testing.T) {
			reqFile := filepath.Join(dir, tc.method+"-request.json")
			os.WriteFile(reqFile, []byte(tc.request), 0600)
			separator := "?"
			if strings.Contains(tc.path, "?") {
				separator = "&"
			}
			first := query(tc.method, "-url", srv.URL+tc.path+separator+"system=example-prod", "-page-size", "1", "-request", reqFile)
			ctxFile := filepath.Join(dir, tc.method+"-context.json")
			os.WriteFile(ctxFile, first.Context, 0600)
			next := query(tc.method, "-context", ctxFile)
			before := dynamicpb.NewMessage(inv.Method(tc.method).Input())
			after := dynamicpb.NewMessage(inv.Method(tc.method).Input())
			protojson.Unmarshal(first.Request, before)
			protojson.Unmarshal(next.Request, after)
			if !proto.Equal(before, after) {
				t.Fatalf("reimport changed outgoing request: %s / %s", first.Request, next.Request)
			}
			if tc.method == "ListConsumers" {
				var token string
				json.Unmarshal(first.Response["nextPageToken"], &token)
				if token == "" {
					t.Fatal("expected bounded continuation")
				}
				page2 := query(tc.method, "-context", ctxFile, "-page-token", token)
				if !bytes.Equal(first.Response["epoch"], page2.Response["epoch"]) || !bytes.Equal(first.Response["range"], page2.Response["range"]) {
					t.Fatal("continuation lost epoch or range")
				}
				var rows []json.RawMessage
				json.Unmarshal(page2.Response["consumers"], &rows)
				if len(rows) != 1 {
					t.Fatal("continuation lost one-row budget")
				}
				selected := false
				for _, l := range page2.Links {
					selected = selected || (l.Kind == "selected_resource" && l.ID == "APP/order-jobs")
				}
				if !selected {
					t.Fatal("selected stream machine link missing")
				}
				os.WriteFile(ctxFile, page2.Context, 0600)
				cleared := query(tc.method, "-context", ctxFile, "-page-token", "")
				var request map[string]any
				json.Unmarshal(cleared.Request, &request)
				if request["pageToken"] != nil {
					t.Fatal("explicit empty token did not clear continuation")
				}
			}
			if tc.method == "ListEvents" {
				var rows []struct{ ID string }
				json.Unmarshal(first.Response["events"], &rows)
				if len(rows) != 1 || rows[0].ID != "991" {
					t.Fatalf("visible resolved event lost: %s", first.Response["events"])
				}
			}
		})
	}
}

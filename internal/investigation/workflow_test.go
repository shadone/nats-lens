package investigation_test

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestAgentWorkflow(t *testing.T) {
	srv := httptest.NewServer(api.NewMux(&api.Handler{Systems: fake.Demo(false), Version: "fixture"}, fstest.MapFS{"index.html": {Data: []byte("fixture UI")}}))
	defer srv.Close()
	dir := t.TempDir()
	profiles := filepath.Join(dir, "profiles.json")
	b, _ := json.Marshal(map[string]inv.Profile{"fixture": {Deployment: srv.URL, Endpoint: srv.URL}})
	if err := os.WriteFile(profiles, b, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) map[string]json.RawMessage {
		t.Helper()
		var out bytes.Buffer
		args = append(args, "-profiles", profiles, "-profile", "fixture")
		if err := inv.Run(context.Background(), args, strings.NewReader(""), &out, &out); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out.String())
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	// Portable URL -> context with server-clock bounds -> facts -> narrower facts.
	for _, s := range []struct{ name, page, method, metric string }{
		{"storage jump", "/capacity?system=example-prod&range=6h", "GetTimeSeries", "cluster.capacity_file_store_bytes"},
		{"CPU spike", "/servers/nats-0?system=example-prod&range=1h", "GetTimeSeries", "server.cpu_percent"},
		{"resolved finding", "/findings?system=example-prod&range=24h&state=resolved", "ListFindings", ""},
	} {
		t.Run(s.name, func(t *testing.T) {
			c := run("context", "-url", srv.URL+s.page)
			if string(c["version"]) != "1" || !bytes.Contains(c["range"], []byte("2026-10-17")) {
				t.Fatalf("server clock: %s", c["range"])
			}
			cb, _ := json.Marshal(c)
			file := filepath.Join(dir, strings.ReplaceAll(s.name, " ", "-")+".json")
			os.WriteFile(file, cb, 0600)
			args := []string{"query", s.method, "-context", file}
			if s.metric != "" {
				args = append(args, "-metric", s.metric)
			}
			r := run(args...)
			if !bytes.Contains(r["request"], []byte("example-prod")) {
				t.Fatal(string(r["request"]))
			}
			var links []inv.EvidenceLink
			json.Unmarshal(r["links"], &links)
			for _, link := range links {
				res, err := http.Get(link.URL)
				if err != nil {
					t.Fatal(err)
				}
				res.Body.Close()
				if res.StatusCode != 200 {
					t.Fatalf("link %s: %d", link.URL, res.StatusCode)
				}
			}
			run(append(args, "-from", "2026-10-17T14:00:00Z", "-to", "2026-10-17T14:20:00Z", "-max-points", "30")...)
			history := run("query", "GetDataHistory", "-context", file)
			if !bytes.Contains(history["response"], []byte("coverage")) {
				t.Fatal(string(history["response"]))
			}
			if s.method == "ListFindings" {
				var response struct {
					Findings []struct {
						ID    string `json:"id"`
						State string `json:"state"`
					}
				}
				json.Unmarshal(r["response"], &response)
				if len(response.Findings) == 0 {
					t.Fatal("fixture has no resolved findings")
				}
				request := filepath.Join(dir, "finding.json")
				os.WriteFile(request, []byte(`{"id":"`+response.Findings[0].ID+`"}`), 0600)
				f := run("query", "GetFinding", "-context", file, "-request", request)
				if !bytes.Contains(f["response"], []byte("transitions")) {
					t.Fatal("missing transitions")
				}
			}
		})
	}
	for _, method := range []string{"ListSystems", "ListMetrics", "ListCheckDefinitions", "GetOverview", "GetCapacity", "ListEvents", "ListStreams", "ListServers", "GetApplications"} {
		run("query", method, "-url", srv.URL+"/?system=example-prod&range=6h", "-page-size", "2")
	}
	events := run("query", "ListEvents", "-url", srv.URL+"/events?system=example-global&range=24h", "-page-size", "2")
	var ep struct {
		Events []struct {
			ID string `json:"id"`
		}
		Next string `json:"nextPageToken"`
	}
	json.Unmarshal(events["response"], &ep)
	if len(ep.Events) == 0 || ep.Next == "" {
		t.Fatalf("fixture pagination: %s", events["response"])
	}
	request := filepath.Join(dir, "event.json")
	os.WriteFile(request, []byte(`{"id":"`+ep.Events[0].ID+`"}`), 0600)
	run("query", "GetEvent", "-url", srv.URL+"/events?system=example-global&range=24h", "-request", request)
	next := run("query", "ListEvents", "-url", srv.URL+"/events?system=example-global&range=24h", "-page-size", "2", "-page-token", ep.Next)
	if bytes.Equal(events["response"], next["response"]) {
		t.Fatal("page token ignored")
	}
}

func TestURLConnectionInfoUsesSelectedSystem(t *testing.T) {
	systems := make(chan string, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			System string `json:"system"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		systems <- req.System
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"version":"fixture","system":"example-staging","now":"2026-10-17T14:30:00Z"}`))
	}))
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "profiles.json")
	b, _ := json.Marshal(map[string]inv.Profile{"fixture": {Deployment: srv.URL, Endpoint: srv.URL}})
	os.WriteFile(file, b, 0600)
	var out bytes.Buffer
	if err := inv.Run(context.Background(), []string{"context", "-profiles", file, "-profile", "fixture", "-url", srv.URL + "/capacity?system=example-staging"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatal(err)
	}
	if len(systems) != 1 {
		t.Fatalf("expected one connection query, got %d", len(systems))
	}
	if got := <-systems; got != "example-staging" {
		t.Fatalf("connection scope: %s", got)
	}
}

func TestOldServerClockContractIsExplicit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"version":"old-fixture","system":"shop"}`))
	}))
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "profiles.json")
	b, _ := json.Marshal(map[string]inv.Profile{"fixture": {Deployment: srv.URL, Endpoint: srv.URL}})
	os.WriteFile(file, b, 0600)
	var out bytes.Buffer
	err := inv.Run(context.Background(), []string{"context", "-profiles", file, "-profile", "fixture"}, strings.NewReader(""), &out, &out)
	if err == nil || !strings.Contains(out.String(), "server lacks required clock/system contract") {
		t.Fatalf("%s %v", out.String(), err)
	}
}

package api_test

import (
	"encoding/json"
	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/webui"
	"google.golang.org/protobuf/encoding/protojson"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shadone/nats-lens/internal/api/fake"
)

func TestCheckCatalogHTTP(t *testing.T) {
	h := &api.Handler{Systems: api.One("default", "Default", fake.New(false))}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/natslens.v1.LensService/ListCheckDefinitions", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	api.NewMux(h, webui.FS()).ServeHTTP(recorder, req)
	res := recorder.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("catalog status = %d, want 200", res.StatusCode)
	}
	var body struct {
		Definitions []struct {
			Code, Description, Remediation string
			Reference                      []struct{ Title, Text string }
		}
		ConfigurationState string
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Definitions) != 91 {
		t.Fatalf("catalog count = %d, want 91", len(body.Definitions))
	}
	if body.ConfigurationState != "demo" {
		t.Errorf("state = %q", body.ConfigurationState)
	}
	seen := map[string]bool{}
	for _, d := range body.Definitions {
		if seen[d.Code] || d.Description == "" || d.Remediation == "" || len(d.Reference) < 2 {
			t.Errorf("incomplete or duplicate reference: %+v", d)
		}
		seen[d.Code] = true
		for code, rules := range map[string][]string{
			"client-006":   {"mute set on the old key no longer applies", "Without it nats-lens stores no `client.auth_error` event"},
			"client-007":   {"row still counts MQTT clients"},
			"client-008":   {"So is nats.py's `0.0.0`", "unknown and not judged", "A minimum of 0 turns a language off", "`min_python` | `python3` | nats.py | 2.9.0"},
			"consumer-015": {"nats-server refuses an update of `max_waiting`"},
			"consumer-018": {"Raft operations", "unknown-health wording", "proposed, not yet confirmed", "Follower reports about other followers", "skipped or failing check runs"},
		} {
			if d.Code != code {
				continue
			}
			var text string
			for _, section := range d.Reference {
				text += section.Text + "\n"
			}
			for _, rule := range rules {
				if !strings.Contains(text, rule) {
					t.Errorf("HTTP catalog %s lost %q", code, rule)
				}
			}
		}

		if d.Code == "client-007" {
			var reference string
			for _, section := range d.Reference {
				reference += section.Text + "\n"
			}
			for _, rule := range []string{"however young", "CONNZ failed", "10th such collection in a row", "resolves at once in a whole collection"} {
				if !strings.Contains(reference, rule) {
					t.Errorf("catalog lost client-007 rule %q", rule)
				}
			}
		}
		if (d.Code == "consumer-009" || d.Code == "stream-008") && (strings.Contains(d.Description, "a replica was rebuilt") || strings.Contains(d.Remediation, "so the replicas resynchronise")) {
			t.Error("creation timestamps presented as a proven cause or guaranteed repair")
		}
	}
	for _, code := range []string{"system-001", "server-025", "server-026"} {
		if !seen[code] {
			t.Errorf("missing built-in %s", code)
		}
	}
}

func TestCheckCatalogSystemIsolation(t *testing.T) {
	private := filepath.Join(t.TempDir(), "remediation.yaml")
	if err := os.WriteFile(private, []byte("server-009: Private guidance example.com/internal\n"), 0600); err != nil {
		t.Fatal(err)
	}
	overridden, err := checks.Load(config.Checks{Disabled: []string{"consumer-009"}, Thresholds: map[string]map[string]string{"consumer-009": {"position_tolerance": "250"}, "server-009": {"lookback": "30m"}}, RemediationFile: private})
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := checks.Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	h := &api.Handler{Systems: api.Systems{
		{Ref: &v1.SystemRef{Id: "shop-a"}, Provider: &storeprov.Provider{Checks: overridden}},
		{Ref: &v1.SystemRef{Id: "shop-b"}, Provider: &storeprov.Provider{Checks: defaults}},
		{Ref: &v1.SystemRef{Id: "unconfigured"}, Provider: &storeprov.Provider{}},
		{Ref: &v1.SystemRef{Id: "demo"}, Provider: fake.New(false)},
	}}
	for _, system := range []string{"shop-a", "shop-b", "unconfigured", "demo", "unknown"} {
		t.Run(system, func(t *testing.T) {
			r := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/natslens.v1.LensService/ListCheckDefinitions", strings.NewReader(`{"system":"`+system+`"}`))
			req.Header.Set("Content-Type", "application/json")
			api.NewMux(h, webui.FS()).ServeHTTP(r, req)
			if system == "unknown" {
				if r.Code != 400 {
					t.Fatalf("unknown system status %d", r.Code)
				}
				return
			}
			if r.Code != 200 {
				t.Fatalf("status %d: %s", r.Code, r.Body.String())
			}
			if strings.Contains(r.Body.String(), "Private guidance") || strings.Contains(r.Body.String(), "SELECT ") {
				t.Fatal("catalog leaked remediation or detection internals")
			}
			var body v1.ListCheckDefinitionsResponse
			if err := protojson.Unmarshal(r.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body.Definitions) != 91 {
				t.Fatalf("count %d", len(body.Definitions))
			}
			var consumer, slow *v1.CheckDefinition
			for _, d := range body.Definitions {
				if d.Code == "consumer-009" {
					consumer = d
				}
				if d.Code == "server-009" {
					slow = d
				}
			}
			if consumer.Thresholds[0].DefaultValue != "100" || slow.Lookback != "15m" {
				t.Fatal("shipped defaults changed with configuration")
			}
			if system == "shop-a" {
				if body.ConfigurationState != "configured" || !consumer.GetDisabled() || consumer.Thresholds[0].GetEffectiveValue() != "250" || slow.GetEffectiveLookback() != "30m" {
					t.Fatalf("override not exposed: %v %v", consumer, slow)
				}
			} else if system == "shop-b" {
				if consumer.GetDisabled() || consumer.Thresholds[0].GetEffectiveValue() != "100" || slow.GetEffectiveLookback() != "15m" {
					t.Fatal("system A overrides leaked into B")
				}
			} else {
				want := "unavailable"
				if system == "demo" {
					want = "demo"
				}
				if body.ConfigurationState != want || consumer.Disabled != nil || consumer.Thresholds[0].EffectiveValue != nil || slow.EffectiveLookback != nil || body.RestartWindow != nil {
					t.Fatalf("invented effective configuration: %v", &body)
				}
			}
		})
	}
}

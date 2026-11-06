package api_test

import (
	"encoding/json"
	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/webui"
	"net/http"
	"net/http/httptest"
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
	if len(body.Definitions) != 88 {
		t.Fatalf("catalog count = %d, want 88", len(body.Definitions))
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
		if d.Code == "consumer-009" && (strings.Contains(d.Description, "a replica was rebuilt") || strings.Contains(d.Remediation, "so the replicas resynchronise")) {
			t.Error("creation timestamps presented as a proven cause or guaranteed repair")
		}
	}
	for _, code := range []string{"system-001", "server-025", "server-026"} {
		if !seen[code] {
			t.Errorf("missing built-in %s", code)
		}
	}
}

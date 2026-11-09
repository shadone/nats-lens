package investigation

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewerPageSizeAndOverrideLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "GetLensInfo") {
			w.Write([]byte(`{"version":"fixture","system":"shop","now":"2026-10-17T14:30:00Z"}`))
			return
		}
		w.Write([]byte(`{"streams":[]}`))
	}))
	defer srv.Close()
	d := t.TempDir()
	p := filepath.Join(d, "profiles.json")
	b, _ := json.Marshal(map[string]Profile{"fixture": {Deployment: srv.URL, Endpoint: srv.URL}})
	os.WriteFile(p, b, 0600)
	request := filepath.Join(d, "request.json")
	os.WriteFile(request, []byte(`{"account":"APP"}`), 0600)
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"query", "ListStreams", "-profiles", p, "-profile", "fixture", "-url", srv.URL + "/streams?system=shop&account=OTHER", "-request", request, "-page-size", "1"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatal(err, out.String())
	}
	var result struct {
		Request map[string]json.RawMessage `json:"request"`
		Links   []EvidenceLink             `json:"links"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	t.Run("page size reaches the existing limit field", func(t *testing.T) {
		if string(result.Request["limit"]) != "1" {
			t.Fatalf("-page-size 1 sent limit=%s; request=%s", result.Request["limit"], result.Request)
		}
	})
	t.Run("source link matches explicit account", func(t *testing.T) {
		u, _ := url.Parse(result.Links[0].URL)
		if got := u.Query().Get("account"); got != "APP" {
			t.Fatalf("request account APP but source link account %q: %s", got, u)
		}
	})
}

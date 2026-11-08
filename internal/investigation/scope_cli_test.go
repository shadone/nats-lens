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

func TestConsumerScopeCLIOverrides(t *testing.T) {
	mux := api.NewMux(&api.Handler{Systems: fake.Demo(false)}, fstest.MapFS{"index.html": {Data: []byte("fixture")}})
	srv := httptest.NewServer(http.HandlerFunc(mux.ServeHTTP))
	defer srv.Close()
	dir := t.TempDir()
	profile := filepath.Join(dir, "profiles.json")
	b, _ := json.Marshal(map[string]inv.Profile{"fixture": {Deployment: srv.URL, Endpoint: srv.URL}})
	os.WriteFile(profile, b, 0600)
	request := filepath.Join(dir, "request.json")
	os.WriteFile(request, []byte(`{"account":"REQUEST","stream":"explicit"}`), 0600)
	for _, tc := range []struct {
		flags           []string
		account, stream string
	}{
		{nil, "APP", "order-jobs"},
		{[]string{"-account", "FLAG"}, "FLAG", "order-jobs"},
		{[]string{"-account", "FLAG", "-request", request}, "REQUEST", "explicit"},
	} {
		var out bytes.Buffer
		args := []string{"query", "ListConsumers", "-profiles", profile, "-profile", "fixture", "-url", srv.URL + "/consumers?system=example-prod&account=OTHER&stream=APP/order-jobs", "-page-size", "1"}
		args = append(args, tc.flags...)
		if err := inv.Run(context.Background(), args, strings.NewReader(""), &out, &out); err != nil {
			t.Fatal(err, out.String())
		}
		var result struct {
			Request struct{ Account, Stream string }
			Context inv.Context
			Links   []struct{ Kind, URL string }
		}
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Request.Account != tc.account || result.Request.Stream != tc.stream || result.Context.Account != tc.account {
			t.Fatalf("flags %v: request=%+v context=%+v", tc.flags, result.Request, result.Context)
		}
		for _, link := range result.Links {
			if link.Kind == "source" && (!strings.Contains(link.URL, "account="+tc.account) || !strings.Contains(link.URL, "stream="+tc.account+"%2F"+tc.stream)) {
				t.Fatal(link.URL)
			}
		}
	}
}

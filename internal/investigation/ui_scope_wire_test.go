package investigation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/fake"
	inv "github.com/shadone/nats-lens/internal/investigation"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestUIScopeMismatchDoesNotBroadenMachineRequest(t *testing.T) {
	srv := httptest.NewServer(api.NewMux(&api.Handler{Systems: fake.Demo(false)}, fstest.MapFS{"index.html": {Data: []byte("fixture")}}))
	defer srv.Close()
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile.json")
	b, _ := json.Marshal(map[string]inv.Profile{"fixture": {Deployment: srv.URL, Endpoint: srv.URL}})
	os.WriteFile(profile, b, 0600)
	type result struct {
		Context      json.RawMessage
		Request      json.RawMessage
		Response     struct{ TotalCount int }
		Availability map[string]inv.Availability
		Links        []inv.EvidenceLink
	}
	query := func(method string, args ...string) result {
		t.Helper()
		var out bytes.Buffer
		all := append([]string{"query", method, "-profiles", profile, "-profile", "fixture"}, args...)
		if err := inv.Run(context.Background(), all, strings.NewReader(""), &out, &out); err != nil {
			t.Fatal(err, out.String())
		}
		var r result
		if json.Unmarshal(out.Bytes(), &r) != nil {
			t.Fatal(out.String())
		}
		return r
	}
	for _, tc := range []struct {
		method, path, request string
		filtered, ui          int
	}{
		{"ListEvents", "/events?system=example-prod&show=all", `{"entity":{"type":"ENTITY_TYPE_ACCOUNT","key":"APP","account":"APP"},"hideRoutine":false,"foldRepeats":false}`, 1107, 1176},
		{"ListFindings", "/findings?system=example-prod", `{"severities":[0],"includeMuted":true}`, 0, 8},
	} {
		t.Run(tc.method, func(t *testing.T) {
			request := filepath.Join(dir, "request.json")
			os.WriteFile(request, []byte(tc.request), 0600)
			first := query(tc.method, "-url", srv.URL+tc.path, "-request", request)
			if first.Availability["uiLinkScope"].Status != "partial" || first.Response.TotalCount != tc.filtered {
				t.Fatalf("scope=%+v rows=%d", first.Availability, first.Response.TotalCount)
			}
			ctx := filepath.Join(dir, "context.json")
			os.WriteFile(ctx, first.Context, 0600)
			again := query(tc.method, "-context", ctx)
			a := dynamicpb.NewMessage(inv.Method(tc.method).Input())
			z := dynamicpb.NewMessage(inv.Method(tc.method).Input())
			protojson.Unmarshal(first.Request, a)
			protojson.Unmarshal(again.Request, z)
			if !proto.Equal(a, z) || again.Response.TotalCount != tc.filtered || again.Availability["uiLinkScope"].Status != "partial" {
				t.Fatal("machine context lost exact predicate")
			}
			for _, link := range first.Links {
				if link.Kind == "source" {
					broad := query(tc.method, "-url", link.URL)
					if broad.Response.TotalCount != tc.ui {
						t.Fatalf("actual UI predicate projection returned %d want %d", broad.Response.TotalCount, tc.ui)
					}
				}
			}
		})
	}
}

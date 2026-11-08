package investigation

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestStorageChangePortablePins(t *testing.T) {
	url := "https://lens.example.com/capacity/storage-change?system=shop&account=APP&from=2026-10-17T13:00:00.000123Z&to=2026-10-17T14:00:00.000456Z&epochA=9007199254740993&epochB=9007199254740994"
	c, err := ContextFromURL(url, "https://lens.example.com", fixtureNow())
	if err != nil {
		t.Fatal(err)
	}
	request, _, err := Prepare("GetStorageChange", c, nil)
	if err != nil {
		t.Fatal(err)
	}
	var r map[string]any
	if err = json.Unmarshal(request, &r); err != nil {
		t.Fatal(err)
	}
	if r["epochA"] != "9007199254740993" || r["epochB"] != "9007199254740994" || r["account"] != "APP" || r["a"] != "2026-10-17T13:00:00.000123Z" {
		t.Fatalf("lost exact scope=%s", request)
	}
	effective, limits, err := effectiveContext("GetStorageChange", c, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(limits) != 0 {
		t.Fatalf("faithful first page marked partial: %v", limits)
	}
	data, _ := json.Marshal(effective)
	imported, err := ParseContext(bytes.NewReader(data), c.Deployment)
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := Prepare("GetStorageChange", imported, nil)
	if err != nil || string(again) != string(request) {
		t.Fatalf("roundtrip=%s %v", again, err)
	}
}
func TestStorageChangeScopeOverridesResetContinuation(t *testing.T) {
	c, err := ContextFromURL("https://lens.example.com/capacity/storage-change?system=shop&from=2026-10-17T13:00:00Z&to=2026-10-17T14:00:00Z", "https://lens.example.com", fixtureNow())
	if err != nil {
		t.Fatal(err)
	}
	c.Query = &BoundQuery{Method: "GetStorageChange", Request: json.RawMessage(`{"system":"shop","a":"2026-10-17T13:00:00Z","b":"2026-10-17T14:00:00Z","epochA":"1","epochB":"2","pageToken":"old"}`)}
	c.System = "other"
	if err = c.rebaseQuery(true, false, false, false); err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	_ = json.Unmarshal(c.Query.Request, &fields)
	if _, ok := fields["epochA"]; ok {
		t.Fatal("old system's collection pin retained")
	}
	if _, ok := fields["pageToken"]; ok {
		t.Fatal("old system's continuation retained")
	}
}

type storagePinnedSource struct{}

func (s storagePinnedSource) Query(ctx context.Context, method string, request []byte) (json.RawMessage, error) {
	if method == "GetLensInfo" {
		return json.RawMessage(`{"version":"fixture","now":"2026-10-17T14:30:00Z","system":"shop"}`), nil
	}
	return json.RawMessage(`{"system":"shop","account":"APP","a":{"epoch":"9007199254740993"},"b":{"epoch":"9007199254740994"}}`), nil
}
func TestStorageChangeReturnedContextBindsResolvedPins(t *testing.T) {
	opener := func(context.Context, OfflineSelection) (*Session, error) {
		return &Session{Source: storagePinnedSource{}}, nil
	}
	var out, diagnostic bytes.Buffer
	args := []string{"query", "GetStorageChange", "-bundle", "fixture", "-system", "shop", "-account", "APP", "-from", "2026-10-17T13:00:00Z", "-to", "2026-10-17T14:00:00Z"}
	if err := RunWithOffline(context.Background(), args, strings.NewReader(""), &out, &diagnostic, opener); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Request map[string]any `json:"request"`
		Context *Context       `json:"context"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Request["epochA"]; ok {
		t.Fatal("actual sent request was rewritten")
	}
	replay, _, err := Prepare("GetStorageChange", result.Context, nil)
	if err != nil {
		t.Fatal(err)
	}
	var r map[string]any
	_ = json.Unmarshal(replay, &r)
	if r["epochA"] != "9007199254740993" || r["epochB"] != "9007199254740994" {
		t.Fatalf("returned context re-resolves observations: %s", replay)
	}
}
func TestStorageChangePinBindingRejectsInconsistentMetadata(t *testing.T) {
	request := []byte(`{"system":"shop","account":"APP","a":"2026-10-17T13:00:00Z","b":"2026-10-17T14:00:00Z"}`)
	for _, response := range []string{`invalid`, `{"system":"OTHER","account":"APP"}`, `{"system":"shop","account":"OTHER"}`, `{"system":"shop","account":"APP","a":{"epoch":"-1"}}`} {
		if _, err := bindStorageComparison(request, []byte(response)); err == nil {
			t.Fatalf("accepted inconsistent replay metadata: %s", response)
		}
	}
	pinned := bytes.Replace(request, []byte(`"system":"shop"`), []byte(`"system":"shop","epochA":"1"`), 1)
	if _, err := bindStorageComparison(pinned, []byte(`{"system":"shop","account":"APP","a":{"epoch":"2"}}`)); err == nil {
		t.Fatal("requested pin overwritten")
	}
	resolved, err := bindStorageComparison([]byte(`{"a":"2026-10-17T13:00:00Z","b":"2026-10-17T14:00:00Z"}`), []byte(`{"system":"shop","b":{"epoch":"2"}}`))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	_ = json.Unmarshal(resolved, &fields)
	if fields["system"] != "shop" || fields["epochB"] != "2" {
		t.Fatal("default system or available pin lost")
	}
	if _, err = bindStorageComparison([]byte(`{}`), []byte(`{}`)); err == nil {
		t.Fatal("unresolved default source accepted")
	}
}

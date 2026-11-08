package investigation

import (
	"bytes"
	"encoding/json"
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

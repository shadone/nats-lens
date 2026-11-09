package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func testMux(t *testing.T) http.Handler {
	t.Helper()
	h, err := newMux(false, nil)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestServeRunsAConnectCall(t *testing.T) {
	res, err := serve(testMux(t), request{
		Method: http.MethodPost,
		URL:    "/api/natslens.v1.LensService/GetLensInfo",
		Header: map[string]string{"Content-Type": "application/json", "Connect-Protocol-Version": "1", "Connect-Timeout-Ms": "45000"},
		Body:   []byte(`{"system":"example-staging"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != http.StatusOK {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
	if ct := res.Header["Content-Type"]; !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content type %q", ct)
	}
	var info struct {
		Demo     bool   `json:"demo"`
		System   string `json:"system"`
		NowFixed bool   `json:"nowFixed"`
	}
	if err := json.Unmarshal(res.Body, &info); err != nil {
		t.Fatal(err)
	}
	if !info.Demo || info.System != "example-staging" || !info.NowFixed {
		t.Errorf("info %+v from %s", info, res.Body)
	}
}

func TestServeAnswersAnUnknownPathWithNotFound(t *testing.T) {
	for _, url := range []string{"/api/natslens.v1.LensService/NoSuchCall", "/index.html"} {
		res, err := serve(testMux(t), request{Method: http.MethodGet, URL: url})
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", url, res.Status)
		}
	}
}

func TestServeTurnsAPanicIntoAnError(t *testing.T) {
	boom := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })
	_, err := serve(boom, request{Method: http.MethodGet, URL: "/api/x"})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err %v, want the panic", err)
	}
	// A URL httptest cannot parse panics in NewRequest; it fails the one call.
	if _, err := serve(testMux(t), request{Method: http.MethodGet, URL: "%zz"}); err == nil {
		t.Fatal("no error for an unparsable URL")
	}
}

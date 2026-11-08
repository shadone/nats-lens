package investigation

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type frozenSource struct{ requests []string }

func (s *frozenSource) Query(_ context.Context, method string, request []byte) (json.RawMessage, error) {
	s.requests = append(s.requests, method+":"+string(request))
	if method == "GetLensInfo" {
		return json.RawMessage(`{"version":"fixture","now":"2026-10-17T14:30:00Z","system":"shop"}`), nil
	}
	return json.RawMessage(`{"metrics":[]}`), nil
}
func TestOfflineCommandClockAndContext(t *testing.T) {
	source := new(frozenSource)
	closed := false
	opener := func(_ context.Context, selection OfflineSelection) (*Session, error) {
		if selection.DB != "closed.duckdb" || selection.System != "shop" {
			t.Fatalf("selection: %+v", selection)
		}
		return &Session{Source: source, Provenance: map[string]string{"identity": "operator assertion"}, Close: func() error { closed = true; return nil }}, nil
	}
	var out, diagnostic bytes.Buffer
	err := RunWithOffline(context.Background(), []string{"query", "ListMetrics", "-db", "closed.duckdb", "-system", "shop", "-collection-interval", "30s"}, strings.NewReader(""), &out, &diagnostic, opener)
	if err != nil {
		t.Fatalf("%v %s", err, out.String())
	}
	var result map[string]json.RawMessage
	if err = json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !closed || len(source.requests) != 2 {
		t.Fatalf("lifecycle %v %v", closed, source.requests)
	}
	if !bytes.Contains(result["context"], []byte("2026-10-17T14:30:00Z")) || !bytes.Contains(result["source"], []byte(`"kind": "offline"`)) {
		t.Fatalf("dataset clock/source lost: %s", out.String())
	}
	if string(result["links"]) != "null" {
		t.Fatalf("fake UI links: %s", result["links"])
	}
	if !bytes.Contains(result["availability"], []byte("runtimeStatus")) {
		t.Fatalf("runtime availability missing: %s", result["availability"])
	}
}

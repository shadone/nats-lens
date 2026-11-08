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
	if method == "ListMetrics" {
		return json.RawMessage(`{"metrics":[]}`), nil
	}
	return json.RawMessage(`{}`), nil
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

func TestOfflineContextAcceptsInertUIOrigin(t *testing.T) {
	source := new(frozenSource)
	opener := func(context.Context, OfflineSelection) (*Session, error) { return &Session{Source: source}, nil }
	for _, args := range [][]string{
		{"query", "ListMetrics", "-bundle", "fixture", "-url", "https://lens.example.com/servers/nats-0?system=shop&range=1h"},
		{"query", "ListMetrics", "-bundle", "fixture", "-context", "-"},
	} {
		c, err := ContextFromURL("https://lens.example.com/servers/nats-0?system=shop&range=1h", "https://lens.example.com", fixtureNow())
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(c)
		var out, diagnostic bytes.Buffer
		err = RunWithOffline(context.Background(), args, bytes.NewReader(data), &out, &diagnostic, opener)
		if err != nil {
			t.Fatalf("inert context origin rejected: %v %s", err, out.String())
		}
	}
}

func TestRequestedAndResolvedBoundsStaySeparate(t *testing.T) {
	source := new(frozenSource)
	opener := func(context.Context, OfflineSelection) (*Session, error) { return &Session{Source: source}, nil }
	var out, diagnostic bytes.Buffer
	args := []string{"query", "GetOverview", "-bundle", "fixture", "-url", "https://lens.example.com/?system=shop&range=1h", "-from", "2026-10-17T14:00:00Z", "-to", "2026-10-17T14:20:00Z"}
	if err := RunWithOffline(context.Background(), args, strings.NewReader(""), &out, &diagnostic, opener); err != nil {
		t.Fatalf("%v %s", err, out.String())
	}
	var result struct {
		Bounds struct {
			Requested struct {
				URLRange string `json:"urlRange"`
				Context  Bounds `json:"context"`
			} `json:"requested"`
			Resolved Bounds `json:"resolved"`
		} `json:"bounds"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Bounds.Requested.URLRange != "1h" || result.Bounds.Requested.Context.End != "2026-10-17T14:30:00Z" || result.Bounds.Resolved.End != "2026-10-17T14:20:00Z" {
		t.Fatalf("requested bounds lost: %s", out.String())
	}
}

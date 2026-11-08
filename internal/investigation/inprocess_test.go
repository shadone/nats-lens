package investigation

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestInProcessUsesHandlerWithoutNetwork(t *testing.T) {
	calls := 0
	s := NewInProcess(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != Path+"/"+Service+"/GetLensInfo" {
			t.Errorf("path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"now":"2026-10-17T14:30:00Z","system":"shop"}`)
	}))
	got, err := s.Query(context.Background(), "GetLensInfo", []byte(`{}`))
	if err != nil || !strings.Contains(string(got), "shop") || calls != 1 {
		t.Fatalf("calls=%d result=%s err=%v", calls, got, err)
	}
	for _, method := range []string{"MuteFinding", "Unknown"} {
		if _, err = s.Query(context.Background(), method, []byte(`{}`)); err == nil {
			t.Fatal("mutation admitted")
		}
	}
	if calls != 1 {
		t.Fatal("disallowed method reached handler")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.Query(ctx, "GetLensInfo", []byte(`{}`)); err == nil {
		t.Fatal("cancelled query admitted")
	}
}

func TestInProcessBudgetsAndSchema(t *testing.T) {
	calls := 0
	source := NewInProcess(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(strings.Repeat("x", MaxResponseBytes+1)))
	}))
	for _, request := range [][]byte{[]byte(`{"unrecognized":true}`), []byte(strings.Repeat("x", MaxInputBytes+1))} {
		if _, err := source.Query(context.Background(), "GetLensInfo", request); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
	if calls != 0 {
		t.Fatal("request bypassed schema/input cap")
	}
	if _, err := source.Query(context.Background(), "GetLensInfo", []byte(`{}`)); err == nil {
		t.Fatal("oversized response accepted")
	}
	if calls != 1 {
		t.Fatal("unexpected extra call")
	}
}

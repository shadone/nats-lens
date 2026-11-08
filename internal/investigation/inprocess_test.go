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

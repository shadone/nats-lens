package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/investigation"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestFocusedTimeWindowsReadOnlyRoute(t *testing.T) {
	p := fake.New(false)
	h := api.NewInvestigationHandler(&api.Handler{Systems: api.One("shop", "Shop", p)})
	r := &v1.TimeRange{Start: timestamppb.New(fake.FixedNow.Add(-24 * time.Hour)), End: timestamppb.New(fake.FixedNow)}
	body, _ := protojson.Marshal(r)
	request := append([]byte(`{"system":"shop","range":`), body...)
	request = append(request, '}')
	req := httptest.NewRequest(http.MethodPost, investigation.Path+"/natslens.v1.LensService/GetTimeWindows", bytes.NewReader(request))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("focused window read unavailable: HTTP %d %s", response.Code, response.Body.String())
	}
	var got struct {
		Range   json.RawMessage
		Windows []json.RawMessage
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	var actualRange v1.TimeRange
	if err := protojson.Unmarshal(got.Range, &actualRange); err != nil || !proto.Equal(&actualRange, r) {
		t.Fatalf("range mismatch: %s %v", got.Range, err)
	}
	want, err := p.GetOverview(req.Context(), &v1.GetOverviewRequest{Range: r})
	if err != nil {
		t.Fatal(err)
	}
	if len(want.Windows) == 0 || len(got.Windows) != len(want.Windows) {
		t.Fatalf("window fixture/response mismatch: %d / %d", len(got.Windows), len(want.Windows))
	}
	for i, raw := range got.Windows {
		var window v1.TimeWindow
		if err := protojson.Unmarshal(raw, &window); err != nil || !proto.Equal(&window, want.Windows[i]) {
			t.Fatalf("window %d differs: %v", i, err)
		}
	}
}

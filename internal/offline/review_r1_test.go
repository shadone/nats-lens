package offline

import (
	"context"
	"encoding/json"
	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/investigation"
	"github.com/shadone/nats-lens/internal/store"
	"testing"
	"time"
)

func TestReviewerConfiguredSummaryEvaluation(t *testing.T) {
	path := closedFixture(t)
	st, err := store.OpenPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err = st.DB().Exec(`INSERT INTO check_runs(epoch,check_code,status,rows,duration_ms) VALUES (9007199254740993,'server-001','ok',0,1)`); err != nil {
		t.Fatal(err)
	}
	set, err := checks.Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Checks: set, Clock: func() time.Time { return time.Date(2026, 10, 17, 14, 29, 1, 0, time.UTC) }}
	src := investigation.NewInProcess(api.NewInvestigationHandler(&api.Handler{Systems: api.One("shop", "", p)}))
	got, err := src.Query(context.Background(), "ListSystems", []byte(`{"stripBuckets":1}`))
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Systems []struct {
			Status string `json:"status"`
		} `json:"systems"`
	}
	if err = json.Unmarshal(got, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Systems) != 1 || response.Systems[0].Status != "STATUS_NO_DATA" {
		t.Fatalf("only one of %d configured checks ran; expected STATUS_NO_DATA, got %s", len(set.Checks()), response.Systems[0].Status)
	}
}

func TestReviewerConfiguredSummaryError(t *testing.T) {
	path := closedFixture(t)
	st, err := store.OpenPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err = st.DB().Exec(`INSERT INTO check_runs(epoch,check_code,status,rows,duration_ms) VALUES (9007199254740993,'server-001','error',0,1)`); err != nil {
		t.Fatal(err)
	}
	set, err := checks.Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Checks: set, Clock: func() time.Time { return time.Date(2026, 10, 17, 14, 29, 1, 0, time.UTC) }}
	src := investigation.NewInProcess(api.NewInvestigationHandler(&api.Handler{Systems: api.One("shop", "", p)}))
	got, err := src.Query(context.Background(), "ListSystems", []byte(`{"stripBuckets":1}`))
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Systems []struct {
			Status string `json:"status"`
		} `json:"systems"`
	}
	if err = json.Unmarshal(got, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Systems) != 1 || response.Systems[0].Status != "STATUS_NO_DATA" {
		t.Fatalf("only one of %d configured checks ran; expected STATUS_NO_DATA, got %s", len(set.Checks()), response.Systems[0].Status)
	}
}

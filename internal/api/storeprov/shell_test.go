package storeprov_test

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// ShellStatus reads GetOverview's freshness, servers and tally over the
// same range: live ranges, past ones and one before the first epoch.
func TestShellStatusMatchesOverview(t *testing.T) {
	clock := confAt(confEpochs).Add(30 * time.Second)
	st, set := conformanceStore(t)
	p := &storeprov.Provider{St: st, Interval: confStep, Live: true, Checks: set, Clock: func() time.Time { return clock }}
	if p.CollectInterval() != confStep {
		t.Errorf("collect interval %v, want %v", p.CollectInterval(), confStep)
	}
	for _, r := range [][2]time.Duration{{0, 2 * confStep}, {0, time.Hour}, {0, 24 * time.Hour}, {3 * time.Hour, time.Hour}, {20 * time.Hour, time.Hour}} {
		end := clock.Add(-r[0])
		tr := &v1.TimeRange{Start: timestamppb.New(end.Add(-r[1])), End: timestamppb.New(end)}
		sh, err := p.ShellStatus(ctx, &v1.GetShellStatusRequest{Range: tr})
		if err != nil {
			t.Fatal(err)
		}
		o, err := p.GetOverview(ctx, &v1.GetOverviewRequest{Range: tr, MaxPoints: 1})
		if err != nil {
			t.Fatal(err)
		}
		if d := apitest.ShellDiff(sh, o); d != "" {
			t.Errorf("range %v back, %v long: %s", r[0], r[1], d)
		}
		if r[0] == 0 && (sh.GetFreshness().GetEpoch() == 0 || len(sh.GetServers()) == 0) {
			t.Errorf("live range %v holds no epoch or servers: %v", r[1], sh)
		}
	}
}

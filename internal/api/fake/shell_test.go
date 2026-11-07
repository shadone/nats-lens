package fake_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Every demo system's ShellStatus reads GetOverview's freshness, servers
// and tally from the range's epoch, in live and past ranges.
func TestShellStatusMatchesOverview(t *testing.T) {
	ctx := context.Background()
	for _, sys := range fake.Demo(false) {
		p := sys.Provider
		if p.CollectInterval() != 30*time.Second {
			t.Errorf("%s: collect interval %v", sys.Ref.GetId(), p.CollectInterval())
		}
		now := p.Now()
		for _, r := range [][2]time.Duration{{0, time.Minute}, {0, time.Hour}, {2 * time.Hour, time.Hour}, {6 * time.Hour, 10 * time.Minute}, {20 * time.Hour, time.Hour}} {
			end := now.Add(-r[0])
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
				t.Errorf("%s, range %v back, %v long: %s", sys.Ref.GetId(), r[0], r[1], d)
			}
		}
	}
}

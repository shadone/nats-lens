package collect

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/shadone/nats-lens/internal/testcluster"
)

// timeGather answers one request with the given replies and times how long
// gather waits with the given stop condition.
func timeGather(t *testing.T, replies []string, done until) time.Duration {
	c := testcluster.Start(t, 1)
	nc := c.Connect(0, testcluster.SysUser, testcluster.SysPass)
	const subj = "review.ping"
	sub, err := nc.Subscribe(subj, func(m *nats.Msg) {
		for _, r := range replies {
			_ = m.Respond([]byte(r))
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe()
	nc.Flush()
	start := time.Now()
	if _, err := gather(context.Background(), nc, subj, nil, 1500*time.Millisecond, done); err != nil {
		t.Fatal(err)
	}
	return time.Since(start)
}

func rep(id, name string) string {
	return `{"server":{"id":"` + id + `","name":"` + name + `"}}`
}

func TestGatherStopsOnDistinctServers(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server per case")
	}
	long := 1200 * time.Millisecond
	cases := []struct {
		name    string
		replies []string
		done    until
		early   bool
	}{
		{"all names", []string{rep("A", "a"), rep("B", "b")}, allNames([]string{"a", "b"}), true},
		{"one name twice", []string{rep("A", "a"), rep("A", "a")}, allNames([]string{"a", "b"}), false},
		{"renamed", []string{rep("A", "a2"), rep("B", "b")}, allNames([]string{"a", "b"}), false},
		{"restart new id same name", []string{rep("A2", "a"), rep("B", "b")}, allNames([]string{"a", "b"}), true},
		{"ids", []string{rep("A", "a"), rep("B", "b")}, allIDs([]string{"A", "B"}), true},
		{"ids restarted", []string{rep("A2", "a"), rep("B", "b")}, allIDs([]string{"A", "B"}), false},
		{"garbage", []string{"xx", "yy"}, allIDs([]string{"A", "B"}), false},
		{"error reply without server", []string{`{"error":{"code":503}}`, rep("B", "b")}, allIDs([]string{"A", "B"}), false},
		{"nil done", []string{rep("A", "a")}, nil, false},
		{"extra server", []string{rep("A", "a"), rep("B", "b"), rep("C", "c")}, allNames([]string{"a"}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := timeGather(t, tc.replies, tc.done)
			if tc.early && d > long {
				t.Errorf("took %s, want early stop", d)
			}
			if !tc.early && d < long {
				t.Errorf("took %s, want full window", d)
			}
		})
	}
}

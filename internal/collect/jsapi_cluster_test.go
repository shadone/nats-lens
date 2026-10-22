package collect

import (
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/shadone/nats-lens/internal/testcluster"
)

// Every server's JSZ account detail reports the account's JetStream API
// total across the cluster, not only the requests it answered: requests
// made through one server show on all three alike once the account stores
// data on any server of the cluster (servers holding data send their
// usage, with the API counters, every 1.5 seconds, and the others take it
// in). Until then each server reports only the requests it answered,
// mostly the meta leader, and the highest is a lower bound. The Subjects
// page's API rate therefore takes one server's increase, never the sum.
func TestJetStreamAPITotalIsClusterWide(t *testing.T) {
	if testing.Short() {
		t.Skip("in-process cluster")
	}
	c := testcluster.Start(t, 3)
	nc := c.Connect(1, testcluster.AppUser, testcluster.AppPass)
	js, err := nc.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := js.AddStream(&nats.StreamConfig{Name: "s", Subjects: []string{"s.>"}, Replicas: 3}); err != nil {
		t.Fatal(err)
	}
	// The servers share the API counters only once the account stores
	// data on a server of the cluster.
	if _, err := js.Publish("s.x", []byte("x")); err != nil {
		t.Fatal(err)
	}
	const asked = 50
	for range asked {
		if _, err := js.AccountInfo(); err != nil {
			t.Fatal(err)
		}
	}
	totals := func() []uint64 {
		var out []uint64
		for _, s := range c.Servers {
			z, err := s.Jsz(&server.JSzOptions{Accounts: true})
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range z.AccountDetails {
				if d.Name == "APP" || d.Id == "APP" {
					out = append(out, d.API.Total)
				}
			}
		}
		return out
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		got := totals()
		same := len(got) == len(c.Servers)
		for _, v := range got {
			same = same && v == got[0] && v > asked
		}
		if same {
			t.Logf("API total on each server: %v", got)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("API totals per server %v, want the same cluster-wide total above %d on each", got, asked)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

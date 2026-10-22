package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/shadone/nats-lens/internal/testcluster"
)

// In a 3-node cluster a client's subscription is listed once, by the
// server the client is connected to, with the account, queue group and
// connection ID of that server's CONNZ row. Route interest is not listed.
// A stream's subjects are listed once, by its leader, under a JetStream
// client that CONNZ does not list.
func TestSubszClusterListsEachSubscriptionOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a 3-node cluster")
	}
	ctx := context.Background()
	c := testcluster.Start(t, 3)
	worker, err := nats.Connect(c.Servers[1].ClientURL(), nats.UserInfo(testcluster.AppUser, testcluster.AppPass),
		nats.Name("order-service"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	for range 2 {
		// Two subscriptions of one connection on the same subject and
		// queue: the store folds them into one row.
		if _, err := worker.QueueSubscribe("orders.created", "order-service", func(*nats.Msg) {}); err != nil {
			t.Fatal(err)
		}
	}
	if err := worker.Flush(); err != nil {
		t.Fatal(err)
	}
	js, err := worker.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := js.AddStream(&nats.StreamConfig{Name: "inventory-changes", Subjects: []string{"inventory.>"}, Replicas: 3}); err != nil {
		t.Fatal(err)
	}
	// Let the interest reach the other servers over the routes, so that
	// a listing of route interest would show.
	time.Sleep(200 * time.Millisecond)

	at := time.Now()
	col := subszCollector(c.Connect(0, testcluster.SysUser, testcluster.SysPass), time.Minute, 10000, &at)
	snap := col.Collect(ctx)

	var found []string
	for _, v := range snap.SortedViews() {
		if v.Subsz == nil {
			t.Fatalf("%s did not answer SUBSZ: %+v", v.Info.Name, v.Status[Subsz])
		}
		if v.Connz == nil {
			t.Fatalf("%s did not answer CONNZ", v.Info.Name)
		}
		clients := map[uint64]*server.ConnInfo{}
		for _, ci := range v.Connz.Conns {
			clients[ci.Cid] = ci
		}
		internal := map[string]int{}
		for _, d := range v.Subsz.Subs {
			ci, isClient := clients[d.Cid]
			if !isClient {
				internal[d.Account+" "+subjectShape(d.Subject)]++
				continue
			}
			if d.Account != ci.Account {
				t.Errorf("%s: subscription %q of cid %d in account %q, CONNZ says %q",
					v.Info.Name, d.Subject, d.Cid, d.Account, ci.Account)
			}
			if strings.HasPrefix(d.Subject, "inventory.") {
				t.Errorf("%s: a stream subject is listed for client cid %d: %+v", v.Info.Name, d.Cid, d)
			}
			if d.Subject == "orders.created" {
				found = append(found, fmt.Sprintf("%s cid=%d account=%s queue=%s name=%s",
					v.Info.Name, d.Cid, d.Account, d.Queue, ci.Name))
			}
		}
		keys := make([]string, 0, len(internal))
		for k := range internal {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s x%d", k, internal[k]))
		}
		t.Logf("%s: total %d, listed %d, num_subscriptions %d; not a CONNZ client: %s",
			v.Info.Name, v.Subsz.Total, len(v.Subsz.Subs), v.Subsz.NumSubs, strings.Join(parts, ", "))
	}
	// Two subscriptions of one connection: listed twice, on nats-1 only.
	if len(found) != 2 || found[0] != found[1] || !strings.HasPrefix(found[0], "nats-1 ") ||
		!strings.HasSuffix(found[0], " account=APP queue=order-service name=order-service") {
		t.Fatalf("orders.created listed as %q, want twice on nats-1, in APP, queue order-service, with the CONNZ row's cid", found)
	}
	// The stream's own subscription is the leader's JetStream client's.
	leader := ""
	for _, v := range snap.SortedViews() {
		for _, d := range v.Subsz.Subs {
			if d.Subject == "inventory.>" && d.Account == "APP" {
				if leader != "" {
					t.Errorf("stream subject listed on %s and %s", leader, v.Info.Name)
				}
				leader = v.Info.Name
			}
		}
	}
	if leader == "" {
		t.Error("the stream's subject is listed by no server")
	}
}

// subjectShape keeps a subject's first two tokens, so that the log groups
// the server's own subscriptions by kind.
func subjectShape(s string) string {
	tokens := strings.SplitN(s, ".", 3)
	if len(tokens) > 2 {
		return tokens[0] + "." + tokens[1] + ".>"
	}
	return s
}

// A server with 10,000 subscriptions answers SUBSZ with all of them, and
// the reply crosses a route and decodes.
func TestSubszTenThousandOverARoute(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a 2-node cluster")
	}
	ctx := context.Background()
	c := testcluster.Start(t, 2)
	app, err := nats.Connect(c.Servers[1].ClientURL(), nats.UserInfo(testcluster.AppUser, testcluster.AppPass))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	for i := range 10000 {
		subj := fmt.Sprintf("orders.%05d.payment-service.status.changed", i)
		if _, err := app.QueueSubscribe(subj, "payment-service", func(*nats.Msg) {}); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.Flush(); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	col := subszCollector(c.Connect(0, testcluster.SysUser, testcluster.SysPass), time.Minute, 10000, &at)
	start := time.Now()
	snap := col.Collect(ctx)
	took := time.Since(start)
	v := snap.Servers[c.Servers[1].ID()]
	if v == nil || v.Subsz == nil {
		t.Fatalf("nats-1 SUBSZ not answered: %+v", snap.Errors)
	}
	if len(v.Subsz.Subs) != 10000 || v.Subsz.Total < 10000 {
		t.Fatalf("listed %d of %d, want 10000", len(v.Subsz.Subs), v.Subsz.Total)
	}
	b, err := json.Marshal(map[string]any{"server": v.Info, "data": v.Subsz})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("nats-1 reply about %d bytes for %d subscriptions (total %d); epoch took %s",
		len(b), len(v.Subsz.Subs), v.Subsz.Total, took)
}

package collect

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func violation(text string) error { return fmt.Errorf("%w: %s", nats.ErrPermissionViolation, text) }

// The watch takes the refusals of the collector's own requests and leaves
// every other error to the connection's handler; an account lookup's
// refusal is the collector's only while its lookup is in flight.
func TestMicroWatchTakesOwnRefusals(t *testing.T) {
	w := newMicroWatch()
	from, wake := w.mark()
	for _, text := range []string{
		`Permissions Violation for Publish to "$SRV.STATS"`,
		`Permissions Violation for Subscription to "_INBOX.abc.srv.*"`,
	} {
		if !w.take(violation(text)) {
			t.Errorf("not taken: %s", text)
		}
	}
	select {
	case <-wake:
	default:
		t.Error("a taken refusal does not wake the exchange")
	}
	for _, err := range []error{
		violation(`Permissions Violation for Subscription to "$JS.EVENT.>"`),
		violation(`Permissions Violation for Subscription to "_INBOX.abc.*"`),
		violation(`Permissions Violation for Publish to "$SYS.REQ.USER.INFO"`),
		errors.New(`nats: slow consumer, messages dropped on "$SRV.STATS"`),
	} {
		if w.take(err) {
			t.Errorf("taken: %v", err)
		}
	}
	w.lookingUp()
	if !w.take(violation(`Permissions Violation for Publish to "$SYS.REQ.USER.INFO"`)) {
		t.Error("the refusal of a lookup in flight is not taken")
	}
	if w.naming(from, "$SRV.STATS") == nil || w.naming(from, "$SRV.INFO") != nil {
		t.Error("naming")
	}
	if now, _ := w.mark(); w.naming(now, "$SRV.STATS") != nil {
		t.Error("a refusal before the mark is named")
	}
}

// A refused request is logged once, not every epoch, and once more when
// the refusal ends.
func TestMicroRefusalLoggedOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	startOrders(t, url, "1.4.2")
	refused, permitted := connectAs(t, url, "shop-nosrv"), connectAs(t, url, "shop-asker")
	var useRefused atomic.Bool
	useRefused.Store(true)
	var buf bytes.Buffer
	at := time.Now()
	c := microCollector(2*time.Second, 5*time.Minute, &at, AccountConn{Label: "SHOP", Conn: func() *nats.Conn {
		if useRefused.Load() {
			return refused
		}
		return permitted
	}})
	c.log = slog.New(slog.NewTextHandler(&buf, nil))
	for range 3 {
		c.Collect(context.Background())
		at = at.Add(30 * time.Second)
	}
	// A refused request is sent again only after refusedRetry.
	useRefused.Store(false)
	at = at.Add(refusedRetry)
	c.Collect(context.Background())
	if n := strings.Count(buf.String(), "micro service requests refused"); n != 1 {
		t.Errorf("refusal logged %d times in 3 epochs:\n%s", n, buf.String())
	}
	if n := strings.Count(buf.String(), "micro service requests permitted again"); n != 1 {
		t.Errorf("end of the refusal logged %d times:\n%s", n, buf.String())
	}
}

// Two labels whose connections are in one account: the account's answers
// are kept once, under the first label, and from the next epoch on the
// second label does not ask at all. A third label in another account is
// asked as usual.
func TestMicroOneAskPerAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	startOrders(t, url, "1.4.2")
	second := connectAs(t, url, "shop")
	before := second.Stats().OutMsgs
	at := time.Now()
	c := microCollector(2*time.Second, 5*time.Minute, &at,
		fixedConn("first", connectAs(t, url, "shop")), fixedConn("second", second), fixedConn("other", connectAs(t, url, "empty")))
	labels := func(ms []MicroAccount) string {
		var out []string
		for _, m := range ms {
			out = append(out, m.Label+"="+m.Account)
		}
		return strings.Join(out, " ")
	}
	if got := labels(c.Collect(context.Background()).Micro); got != "first=SHOP other=EMPTY" {
		t.Errorf("first epoch: %s", got)
	}
	afterFirst := second.Stats().OutMsgs
	at = at.Add(30 * time.Second)
	if got := labels(c.Collect(context.Background()).Micro); got != "first=SHOP other=EMPTY" {
		t.Errorf("second epoch: %s", got)
	}
	if afterFirst == before || second.Stats().OutMsgs != afterFirst {
		t.Errorf("the second label sent %d messages in the first epoch and %d in the second, want some and none",
			afterFirst-before, second.Stats().OutMsgs-afterFirst)
	}
}

// A refused account lookup is not made again within ten minutes, and is
// made again after them; the services are asked under the label meanwhile.
func TestMicroLookupRetriedAfterRefusal(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	startOrders(t, url, "1.4.2")
	at := time.Now()
	c := microCollector(2*time.Second, 5*time.Minute, &at, fixedConn("shop-label", connectAs(t, url, "shop-nolookup")))
	m := c.Collect(context.Background()).Micro[0]
	st := c.microAccounts["shop-label"]
	if m.Account != "shop-label" || askedOf(m) != "INFO ok 1, STATS ok 1" || !st.lookupDeniedAt.Equal(at) {
		t.Fatalf("first epoch: %s under %q, refused at %v", askedOf(m), m.Account, st.lookupDeniedAt)
	}
	first := at
	at = at.Add(5 * time.Minute)
	c.Collect(context.Background())
	if !st.lookupDeniedAt.Equal(first) {
		t.Errorf("asked again after 5 minutes: refused at %v", st.lookupDeniedAt)
	}
	at = at.Add(6 * time.Minute)
	c.Collect(context.Background())
	if !st.lookupDeniedAt.Equal(at) {
		t.Errorf("not asked again after 11 minutes: refused at %v", st.lookupDeniedAt)
	}
}

// A label no longer configured is forgotten, and everything is when no
// account connection is left.
func TestMicroForgetsUnconfiguredLabels(t *testing.T) {
	at := time.Now()
	c := microCollector(100*time.Millisecond, 5*time.Minute, &at, AccountConn{Label: "old"})
	c.Collect(context.Background())
	c.SetAccountConns([]AccountConn{{Label: "new"}})
	c.Collect(context.Background())
	if _, ok := c.microAccounts["old"]; ok || len(c.microAccounts) != 1 {
		t.Errorf("states %v", c.microAccounts)
	}
	c.microKnown["SHOP"] = map[instanceKey]int{{"orders", "A"}: 0}
	c.SetAccountConns(nil)
	c.Collect(context.Background())
	if len(c.microAccounts) != 0 || len(c.microKnown) != 0 {
		t.Errorf("without account connections: states %v, known %v", c.microAccounts, c.microKnown)
	}
}

// A label that stands in for its account, unresolved and not connected,
// does not hide a later label whose account the server names the same.
func TestMicroUnresolvedLabelHidesNoAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	startOrders(t, url, "1.4.2")
	at := time.Now()
	c := microCollector(2*time.Second, 5*time.Minute, &at, AccountConn{Label: "SHOP"}, fixedConn("good", connectAs(t, url, "shop")))
	for range 2 {
		var got []string
		for _, m := range c.Collect(context.Background()).Micro {
			got = append(got, m.Label+"="+m.Account+" "+askedOf(m))
		}
		if s := strings.Join(got, "; "); !strings.Contains(s, "good=SHOP") || !strings.Contains(s, "STATS ok 1") {
			t.Errorf("epoch: %s", s)
		}
		at = at.Add(30 * time.Second)
	}
}

// A refused request is not sent again for ten minutes: each epoch in
// between records it as an error with the server's text and when it is
// sent again, and the connection publishes nothing; after ten minutes it
// is sent again.
func TestMicroRefusedRequestBacksOff(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	startOrders(t, url, "1.4.2")
	nc := connectAs(t, url, "shop-nosrv")
	at := time.Now()
	c := microCollector(time.Second, 5*time.Minute, &at, fixedConn("SHOP", nc))
	c.Collect(context.Background())
	sent := nc.Stats().OutMsgs
	for range 3 {
		at = at.Add(30 * time.Second)
		m := c.Collect(context.Background()).Micro[0]
		for _, a := range m.Asked {
			if a.State != MicroError || !strings.Contains(a.Error, `Permissions Violation for Publish to "`+a.Request.Subject()+`"`) ||
				!strings.Contains(a.Error, "not sent: refused at") {
				t.Errorf("held %s: %s %q", a.Request, a.State, a.Error)
			}
		}
	}
	if n := nc.Stats().OutMsgs - sent; n != 0 {
		t.Errorf("%d messages sent within ten minutes of the refusal", n)
	}
	at = at.Add(refusedRetry)
	m := c.Collect(context.Background()).Micro[0]
	if nc.Stats().OutMsgs == sent || strings.Contains(askedOf(m)+m.Asked[0].Error, "not sent") {
		t.Errorf("not sent again after ten minutes: %+v", m.Asked)
	}
}

// A hold on a refused request ends when the label's connection changes:
// a redial with new credentials is asked at once, not after the hold.
func TestMicroHoldEndsWithANewConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	startOrders(t, url, "1.4.2")
	refused, permitted := connectAs(t, url, "shop-nosrv"), connectAs(t, url, "shop-asker")
	var useRefused atomic.Bool
	useRefused.Store(true)
	at := time.Now()
	c := microCollector(time.Second, 5*time.Minute, &at, AccountConn{Label: "SHOP", Conn: func() *nats.Conn {
		if useRefused.Load() {
			return refused
		}
		return permitted
	}})
	c.Collect(context.Background())
	useRefused.Store(false)
	at = at.Add(30 * time.Second)
	if got := askedOf(c.Collect(context.Background()).Micro[0]); got != "INFO ok 1, STATS ok 1" {
		t.Errorf("after the new connection: %s", got)
	}
}

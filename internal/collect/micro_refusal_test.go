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
	useRefused.Store(false)
	c.Collect(context.Background())
	if n := strings.Count(buf.String(), "micro service requests refused"); n != 1 {
		t.Errorf("refusal logged %d times in 3 epochs:\n%s", n, buf.String())
	}
	if n := strings.Count(buf.String(), "micro service requests permitted again"); n != 1 {
		t.Errorf("end of the refusal logged %d times:\n%s", n, buf.String())
	}
}

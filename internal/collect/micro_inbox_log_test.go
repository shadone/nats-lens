package collect

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// A user refused its reply inbox is logged once, not every epoch,
// although each epoch's refusal names another inbox.
func TestMicroInboxRefusalLoggedOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	url := startMicroServer(t)
	startOrders(t, url, "1.4.2")
	nc := connectAs(t, url, "shop-noinbox")
	var buf bytes.Buffer
	at := time.Now()
	c := microCollector(time.Second, 5*time.Minute, &at, fixedConn("SHOP", nc))
	c.log = slog.New(slog.NewTextHandler(&buf, nil))
	for range 4 {
		c.Collect(context.Background())
		at = at.Add(30 * time.Second)
	}
	if n := strings.Count(buf.String(), "micro service requests refused"); n != 1 {
		t.Errorf("refusal logged %d times in 4 epochs:\n%s", n, buf.String())
	}
}

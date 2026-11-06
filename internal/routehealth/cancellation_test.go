package routehealth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRoutesContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := 4 * time.Millisecond
	rows, err := RoutesContext(ctx, []Row{{Server: "nats-0", Remote: "nats-1", RTT: &d}}, nil, time.Now(), 30*time.Second, nil, nil)
	if !errors.Is(err, context.Canceled) || rows != nil {
		t.Fatalf("rows %v, err %v", rows, err)
	}
}

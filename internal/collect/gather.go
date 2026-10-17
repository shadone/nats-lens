package collect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// answered holds the distinct servers that replied to one request, by ID
// and by name. A server that replies twice counts once.
type answered struct {
	ids   map[string]bool
	names map[string]bool
}

// until decides when a gather has heard from enough servers to stop before
// the window closes. A nil until waits for the whole window.
type until func(a answered) bool

// allNames stops once every name in want has replied; an empty want never stops early.
func allNames(want []string) until {
	if len(want) == 0 {
		return nil
	}
	return func(a answered) bool {
		for _, n := range want {
			if !a.names[n] {
				return false
			}
		}
		return true
	}
}

// allIDs stops once every server ID in want has replied; an empty want never stops early.
func allIDs(want []string) until {
	if len(want) == 0 {
		return nil
	}
	return func(a answered) bool {
		for _, id := range want {
			if !a.ids[id] {
				return false
			}
		}
		return true
	}
}

// replier reads only the identity of the server that sent a reply. Every
// reply, enveloped or the STATSZ message, carries it under "server".
type replier struct {
	Server struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"server"`
}

// gather publishes one request to a subject every server listens on and
// collects replies until the window closes or done reports that the
// expected servers have all answered. Running out of time is the normal end
// of a gather, not an error.
func gather(ctx context.Context, nc *nats.Conn, subject string, body []byte, window time.Duration, done until) ([][]byte, error) {
	if nc == nil {
		return nil, fmt.Errorf("gather %s: no connection", subject)
	}
	inbox := nc.NewInbox()
	sub, err := nc.SubscribeSync(inbox)
	if err != nil {
		return nil, fmt.Errorf("subscribe %s: %w", subject, err)
	}
	defer func() { _ = sub.Unsubscribe() }()
	// A JSZ reply with every stream and consumer can be large; never drop
	// replies because of the client's default pending limits.
	if err := sub.SetPendingLimits(-1, -1); err != nil {
		return nil, fmt.Errorf("pending limits: %w", err)
	}
	if err := nc.PublishRequest(subject, inbox, body); err != nil {
		return nil, fmt.Errorf("publish %s: %w", subject, err)
	}

	wctx, cancel := context.WithTimeout(ctx, window)
	defer cancel()
	var out [][]byte
	seen := answered{ids: map[string]bool{}, names: map[string]bool{}}
	for done == nil || !done(seen) {
		msg, err := sub.NextMsgWithContext(wctx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				break
			}
			return out, fmt.Errorf("gather %s: %w", subject, err)
		}
		if len(msg.Data) == 0 {
			continue
		}
		out = append(out, msg.Data)
		var r replier
		if json.Unmarshal(msg.Data, &r) == nil && r.Server.ID != "" {
			seen.ids[r.Server.ID] = true
			seen.names[r.Server.Name] = true
		}
	}
	return out, nil
}

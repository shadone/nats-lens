package collect

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

// microQuiet ends a $SRV request once no reply came for this long after
// the last one. Every instance answers from its own subscription as soon
// as the request reaches it, so the replies of one request arrive within a
// few milliseconds of each other on one server and within tens of
// milliseconds across routes and gateways; a quarter second is several
// times that and still keeps an epoch short.
const microQuiet = 250 * time.Millisecond

// The status header and value of the server's no responders reply.
const (
	statusHeader       = "Status"
	noRespondersStatus = "503"
)

// exchanged is how one request of an exchange went.
type exchanged struct {
	// replies are the reply bodies, in arrival order.
	replies [][]byte
	// noResponders is set when the server answered that nothing listens
	// on the subject.
	noResponders bool
	// err is a permissions violation for the request or its reply inbox,
	// a failed publish, or a connection lost while waiting.
	err error
}

// exchange sends each request in reqs (token to subject) over nc with its
// own reply subject under one fresh inbox, and gathers the replies until
// deadline, or until every request has ended and no reply came for quiet.
// A request ends with its first reply, a no responders status, or a
// permissions violation. A violation arrives as an asynchronous -ERR; a
// flush after each publish makes it arrive before the next step, and it
// is read from nc.LastError by the subject it names.
func exchange(ctx context.Context, nc *nats.Conn, reqs map[string]string, deadline time.Time,
	quiet time.Duration) map[string]*exchanged {
	out := make(map[string]*exchanged, len(reqs))
	for tok := range reqs {
		out[tok] = &exchanged{}
	}
	failAll := func(err error) map[string]*exchanged {
		for _, r := range out {
			if r.err == nil && len(r.replies) == 0 && !r.noResponders {
				r.err = err
			}
		}
		return out
	}
	inbox := nc.NewInbox()
	pattern := inbox + ".*"
	before := nc.LastError()
	// A callback rather than a synchronous subscription: NextMsg turns a
	// no responders status into an error without the message, which
	// would not say which request it answers.
	msgs := make(chan *nats.Msg, 64)
	done := make(chan struct{})
	defer close(done)
	sub, err := nc.Subscribe(pattern, func(m *nats.Msg) {
		select {
		case msgs <- m:
		case <-done:
		}
	})
	if err != nil {
		return failAll(fmt.Errorf("subscribe %s: %w", pattern, err))
	}
	defer func() { _ = sub.Unsubscribe() }()
	if err := sub.SetPendingLimits(-1, -1); err != nil {
		return failAll(fmt.Errorf("pending limits: %w", err))
	}
	if err := flushUntil(nc, deadline); err != nil {
		return failAll(err)
	}
	if v := newViolation(nc, before, pattern); v != nil {
		// Replies could not arrive: nothing is sent.
		return failAll(v)
	}
	waiting := map[string]bool{}
	for _, tok := range sortedKeys(reqs) {
		subject := reqs[tok]
		before := nc.LastError()
		if err := nc.PublishRequest(subject, inbox+"."+tok, nil); err != nil {
			out[tok].err = fmt.Errorf("publish %s: %w", subject, err)
			continue
		}
		if err := flushUntil(nc, deadline); err != nil {
			out[tok].err = err
			continue
		}
		if v := newViolation(nc, before, subject); v != nil {
			out[tok].err = v
			continue
		}
		waiting[tok] = true
	}
	var last time.Time
	for {
		wait := time.Until(deadline)
		if len(waiting) == 0 {
			wait = min(wait, time.Until(last.Add(quiet)))
		}
		if wait <= 0 {
			return out
		}
		timer := time.NewTimer(wait)
		var msg *nats.Msg
		select {
		case msg = <-msgs:
		case <-timer.C:
		case <-ctx.Done():
		}
		timer.Stop()
		if ctx.Err() != nil {
			return failAll(fmt.Errorf("wait for replies: %w", ctx.Err()))
		}
		if msg == nil {
			if nc.IsClosed() {
				return failAll(errors.New("wait for replies: connection closed"))
			}
			continue
		}
		tok := strings.TrimPrefix(msg.Subject, inbox+".")
		r, ok := out[tok]
		if !ok {
			continue
		}
		delete(waiting, tok)
		if len(msg.Data) == 0 && msg.Header.Get(statusHeader) == noRespondersStatus {
			r.noResponders = true
			continue
		}
		r.replies = append(r.replies, msg.Data)
		last = time.Now()
	}
}

// flushUntil flushes nc, waiting at most until deadline.
func flushUntil(nc *nats.Conn, deadline time.Time) error {
	wait := time.Until(deadline)
	if wait <= 0 {
		return errors.New("no time left to flush")
	}
	if err := nc.FlushTimeout(wait); err != nil {
		return fmt.Errorf("flush: %w", err)
	}
	return nil
}

// newViolation returns nc's last error when it is a permissions violation
// naming subject that arrived after before was read, and nil otherwise.
func newViolation(nc *nats.Conn, before error, subject string) error {
	err := nc.LastError()
	if err == nil || !errors.Is(err, nats.ErrPermissionViolation) {
		return nil
	}
	// nats.go makes a new error value for every violation, so an equal
	// value is the violation that was already there.
	if err == before {
		return nil
	}
	if !strings.Contains(err.Error(), `"`+subject+`"`) {
		return nil
	}
	return err
}

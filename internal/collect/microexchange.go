package collect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
)

// microQuiet ends a $SRV request once no reply came for this long after
// the last one. Every instance answers from its own subscription as soon
// as the request reaches it, so the replies of one request arrive within a
// few milliseconds of each other on one server and within tens of
// milliseconds across routes and gateways; a quarter second is several
// times that and still keeps an epoch short.
const microQuiet = 250 * time.Millisecond

// microKnownGrace is the shortest time known instances are still waited
// for after an instance not known replied.
const microKnownGrace = time.Second

// later returns the later of a and b.
func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

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
// permissions violation. A violation arrives as an asynchronous -ERR: a
// flush after the subscription and after each publish makes the server's
// answer arrive before the next step, nc.LastError then holds it, and w
// receives it from the connection's error handler, which also catches one
// that LastError lost to a later error. The quiet period starts only once
// every instance in known has replied to each request of identity, so an
// instance known to answer is waited for until deadline however late it
// replies. Once an instance not in known replies (a restart's new ID, or
// a new instance), the known ones are waited for only until knownCap: the
// later of microKnownGrace after that reply and half the time from the
// start of the exchange to deadline, but never later than a quarter of
// that time (at least quiet) before deadline, which the INFO a new
// instance calls for keeps.
func exchange(ctx context.Context, nc *nats.Conn, w *microWatch, reqs map[string]string, deadline time.Time,
	quiet time.Duration, known map[instanceKey]bool, identity ...string) map[string]*exchanged {
	begun := time.Now()
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
	from, wake := w.mark()
	refused := func(before error, subject string) error {
		if v := newViolation(nc, before, subject); v != nil {
			return v
		}
		return w.naming(from, subject)
	}
	inbox := nc.NewInbox() + microInboxMark
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
	if v := refused(before, pattern); v != nil {
		// Replies could not arrive: nothing is sent.
		return failAll(v)
	}
	waiting := map[string]bool{}
	for _, tok := range sortedKeys(reqs) {
		subject := reqs[tok]
		before := nc.LastError()
		if subject == userInfoSubject {
			w.lookingUp()
		}
		if err := nc.PublishRequest(subject, inbox+"."+tok, nil); err != nil {
			out[tok].err = fmt.Errorf("publish %s: %w", subject, err)
			continue
		}
		if err := flushUntil(nc, deadline); err != nil {
			out[tok].err = err
			continue
		}
		if v := refused(before, subject); v != nil {
			out[tok].err = v
			continue
		}
		waiting[tok] = true
	}
	// outstanding are the known instances each request still waits for.
	outstanding := map[string]map[instanceKey]bool{}
	for _, tok := range identity {
		if waiting[tok] && len(known) > 0 {
			outstanding[tok] = maps.Clone(known)
		}
	}
	// checkRefused ends the waiting requests that a refusal taken by w
	// names; a refused inbox ends them all.
	checkRefused := func() bool {
		if v := w.naming(from, pattern); v != nil {
			failAll(v)
			return true
		}
		for tok := range waiting {
			if v := w.naming(from, reqs[tok]); v != nil && len(out[tok].replies) == 0 {
				out[tok].err = v
				delete(waiting, tok)
				delete(outstanding, tok)
			}
		}
		return false
	}
	var last time.Time
	var knownCap time.Time // zero until an unknown instance replied
	for {
		if checkRefused() {
			return out
		}
		if !knownCap.IsZero() && !time.Now().Before(knownCap) {
			clear(outstanding)
		}
		wait := time.Until(deadline)
		if len(outstanding) > 0 && !knownCap.IsZero() {
			wait = min(wait, time.Until(knownCap))
		}
		if len(waiting) == 0 && len(outstanding) == 0 {
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
		case <-wake:
			_, wake = w.mark()
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
			delete(outstanding, tok)
			continue
		}
		r.replies = append(r.replies, msg.Data)
		last = time.Now()
		if left := outstanding[tok]; left != nil {
			var id micro.ServiceIdentity
			if json.Unmarshal(msg.Data, &id) == nil {
				k := instanceKey{id.Name, id.ID}
				delete(left, k)
				// An instance not known before is most often a restart
				// whose old ID will not answer again: the known ones are
				// waited for only until the cap, so that the INFO it
				// calls for has time.
				if !known[k] && knownCap.IsZero() {
					window := deadline.Sub(begun)
					knownCap = later(last.Add(microKnownGrace), begun.Add(window/2))
					if latest := deadline.Add(-max(window/4, quiet)); knownCap.After(latest) {
						knownCap = latest
					}
				}
			}
			if len(left) == 0 {
				delete(outstanding, tok)
			}
		}
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

package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
)

// queueSize is how many received messages wait for decoding. A full queue
// drops messages and records how many as a collector.messages_dropped event.
const queueSize = 65536

// maxBuffered caps the events kept for a retry after a failed write.
const maxBuffered = 100000

// seenFor is how long a payload ID is remembered to drop its second copy.
const seenFor = time.Minute

// Options configure a Capture.
type Options struct {
	Writer Writer
	// APIAudit stores every API audit advisory; when false they are only
	// used to name the client behind stream and consumer actions.
	APIAudit bool
	// AccountConnections captures the account connection counts every
	// server sends for every account every 30 seconds.
	AccountConnections bool
	FlushInterval      time.Duration
	// NotConnectedAfter is how long a collector connection may be without
	// a server before a collector.not_connected event is recorded; zero
	// means DefaultNotConnectedAfter.
	NotConnectedAfter time.Duration
	BatchSize         int
	Log               *slog.Logger
	// Churn collapses short-lived connections that close themselves.
	Churn ChurnOptions
}

// incoming is a received message, or an event the collector made itself.
type incoming struct {
	account string // account label for JetStream messages
	sys     bool
	subject string
	data    []byte
	at      time.Time
	event   *Event
}

// Capture subscribes to event subjects, decodes and batches what arrives,
// and writes it through the Writer.
type Capture struct {
	o       Options
	ch      chan incoming
	dropped atomic.Int64
	now     func() time.Time
	lookups accountLookups
	finalBy atomic.Pointer[time.Time]
	// resolveWait is the wait between account name requests.
	resolveWait time.Duration
}

// New returns a Capture. Call Run to start writing.
func New(o Options) *Capture {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.BatchSize <= 0 {
		o.BatchSize = 500
	}
	if o.FlushInterval <= 0 {
		o.FlushInterval = time.Second
	}
	if o.NotConnectedAfter <= 0 {
		o.NotConnectedAfter = DefaultNotConnectedAfter
	}
	return &Capture{o: o, ch: make(chan incoming, queueSize), now: time.Now, resolveWait: resolveRetry,
		lookups: accountLookups{requests: map[string]int{}, denied: map[string]bool{}}}
}

// SubscribeSys subscribes nc, a system account connection, to SysSubjects.
func (c *Capture) SubscribeSys(nc *nats.Conn) error {
	return c.subscribe(nc, SysSubjects(c.o.AccountConnections), func(m *nats.Msg, at time.Time) incoming {
		return incoming{sys: true, subject: m.Subject, data: m.Data, at: at}
	})
}

// SubscribeAccount subscribes nc, a connection in the JetStream account
// configured as label, to JSSubjects. Events carry the account name the
// servers use, which advisories and connection events name too; it is
// asked of the server once connected, and label stands in until then.
func (c *Capture) SubscribeAccount(label string, nc *nats.Conn) error {
	name := &atomic.Pointer[string]{}
	name.Store(&label)
	go c.resolveAccount(nc, label, name)
	return c.subscribe(nc, JSSubjects, func(m *nats.Msg, at time.Time) incoming {
		return incoming{account: *name.Load(), subject: m.Subject, data: m.Data, at: at}
	})
}

// userInfoSubject answers which account a connection's user is in.
const userInfoSubject = "$SYS.REQ.USER.INFO"

// resolveAttempts is how many account name requests are made per account
// label over the life of the process, across connections and redials.
const resolveAttempts = 5

// resolveRetry is the wait between account name requests.
const resolveRetry = 2 * time.Second

// accountLookups tracks account name requests per label for the process.
type accountLookups struct {
	mu       sync.Mutex
	requests map[string]int
	denied   map[string]bool
}

// next reports whether another request may be made for label and counts it.
func (l *accountLookups) next(label string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.denied[label] || l.requests[label] >= resolveAttempts {
		return false
	}
	l.requests[label]++
	return true
}

// deny stops requests for label; it returns true the first time.
func (l *accountLookups) deny(label string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	first := !l.denied[label]
	l.denied[label] = true
	return first
}

func (c *Capture) resolveDenied(label string) bool {
	c.lookups.mu.Lock()
	defer c.lookups.mu.Unlock()
	return c.lookups.denied[label]
}

func (c *Capture) resolveRequests(label string) int {
	c.lookups.mu.Lock()
	defer c.lookups.mu.Unlock()
	return c.lookups.requests[label]
}

// denyResolve records that the server refused the account name request
// for label, so it is not made again in this process.
func (c *Capture) denyResolve(label string) {
	if c.lookups.deny(label) {
		c.o.Log.Info("account name lookup not permitted; events of this account carry the configured label",
			"label", label, "subject", userInfoSubject)
	}
}

// isUserInfoDenied reports whether err is the server refusing a request to
// userInfoSubject.
func isUserInfoDenied(err error) bool {
	return errors.Is(err, nats.ErrPermissionViolation) && strings.Contains(err.Error(), userInfoSubject)
}

// isInboxDenied reports whether err is the server refusing a subscription
// to a reply inbox. An account connection makes no request but the account
// name lookup, so there it means the lookup cannot get its answer.
func isInboxDenied(err error) bool {
	return errors.Is(err, nats.ErrPermissionViolation) && strings.Contains(err.Error(), `"`+nats.InboxPrefix)
}

// lookupDenied reports whether err refuses the account name lookup of the
// connection labelled label: the request itself, or, on a connection that
// makes the lookup, its reply inbox.
func (c *Capture) lookupDenied(label string, err error) bool {
	return isUserInfoDenied(err) || (isInboxDenied(err) && c.resolveRequests(label) > 0)
}

// resolveAccount asks the server which account nc's user is in, until it
// gets an answer, nc is closed, the server refuses, or the label used up
// its requests; the last is logged as a warning for every connection.
func (c *Capture) resolveAccount(nc *nats.Conn, label string, name *atomic.Pointer[string]) {
	var err error
	for !nc.IsClosed() {
		if !nc.IsConnected() {
			sleepUnlessClosed(nc, time.Second)
			continue
		}
		if !c.lookups.next(label) {
			// The requests for this label are used up, by this connection
			// or earlier ones; say so for every connection it affects.
			if !c.resolveDenied(label) {
				c.o.Log.Warn("account name not resolved; events use the configured label",
					"label", label, "requests", resolveAttempts, "err", err)
			}
			return
		}
		var msg *nats.Msg
		msg, err = nc.Request(userInfoSubject, nil, max(c.resolveWait, time.Second))
		if c.lookupDenied(label, err) || c.lookupDenied(label, nc.LastError()) {
			c.denyResolve(label)
			return
		}
		if err == nil {
			var r struct {
				Data struct {
					Account string `json:"account"`
				} `json:"data"`
			}
			if err = json.Unmarshal(msg.Data, &r); err == nil && r.Data.Account != "" {
				if r.Data.Account != label {
					c.o.Log.Info("account label maps to account", "label", label, "account", r.Data.Account)
				}
				name.Store(&r.Data.Account)
				return
			}
		}
		sleepUnlessClosed(nc, c.resolveWait)
	}
}

// sleepUnlessClosed waits d, returning early once nc is closed.
func sleepUnlessClosed(nc *nats.Conn, d time.Duration) {
	for end := time.Now().Add(d); time.Now().Before(end) && !nc.IsClosed(); {
		time.Sleep(min(50*time.Millisecond, time.Until(end)))
	}
}

func (c *Capture) subscribe(nc *nats.Conn, subjects []string, wrap func(*nats.Msg, time.Time) incoming) error {
	for _, s := range subjects {
		if _, err := nc.Subscribe(s, func(m *nats.Msg) { c.enqueue(wrap(m, c.now())) }); err != nil {
			return fmt.Errorf("subscribe %s: %w", s, err)
		}
	}
	// Flush makes a permissions error on a subscription show up now
	// rather than later through the error handler.
	if nc.IsConnected() {
		if err := nc.Flush(); err != nil {
			return fmt.Errorf("flush subscriptions: %w", err)
		}
	}
	return nil
}

func (c *Capture) enqueue(in incoming) {
	select {
	case c.ch <- in:
	default:
		c.dropped.Add(1)
	}
}

// Record queues an event the collector made itself.
func (c *Capture) Record(ev Event) {
	c.enqueue(incoming{event: &ev})
}

// maxRetryWait caps the wait between write attempts while writes fail.
const maxRetryWait = 30 * time.Second

// Run decodes, correlates and writes events until ctx is done, then writes
// what is left. After a failed write the next attempt waits for the ticker,
// with the wait doubling up to maxRetryWait, instead of following every
// message.
func (c *Capture) Run(ctx context.Context) error {
	ticker := time.NewTicker(c.o.FlushInterval)
	defer ticker.Stop()
	cor := newCorrelator()
	ch := newChurn(c.o.Churn)
	seen := map[string]time.Time{}
	var buf []Event
	var lastAt, retryAt time.Time
	wait := time.Duration(0)
	flush := func(wctx context.Context) {
		if n := c.dropped.Swap(0); n > 0 {
			buf = append(buf, droppedEvent(n, c.now()))
		}
		if len(buf) == 0 {
			return
		}
		if err := c.o.Writer.WriteEvents(wctx, buf); err != nil {
			wait = min(max(2*wait, c.o.FlushInterval), maxRetryWait)
			retryAt = c.now().Add(wait)
			c.o.Log.Error("events not stored", "count", len(buf), "retry_in", wait, "err", err)
			if len(buf) > maxBuffered {
				c.dropped.Add(int64(len(buf)))
				buf = nil
			}
			return
		}
		wait, retryAt = 0, time.Time{}
		buf = buf[:0]
	}
	receive := func(in incoming) {
		for _, e := range c.handle(cor, seen, in) {
			buf = append(buf, ch.add(e)...)
		}
		if in.event == nil {
			lastAt = in.at
		}
	}
	for {
		select {
		case <-ctx.Done():
			// Drain what was already received, then write everything.
			for done := false; !done; {
				select {
				case in := <-c.ch:
					receive(in)
				default:
					done = true
				}
			}
			buf = append(buf, cor.flush()...)
			buf = append(buf, ch.flush()...)
			if n := c.dropped.Swap(0); n > 0 {
				buf = append(buf, droppedEvent(n, c.now()))
			}
			c.finalFlush(ctx, buf)
			return nil
		case in := <-c.ch:
			receive(in)
			if len(buf) >= c.o.BatchSize && retryAt.IsZero() {
				flush(ctx)
			}
		case <-ticker.C:
			now := c.now()
			buf = append(buf, cor.expire(expireClock(len(c.ch), lastAt, now))...)
			buf = append(buf, ch.expire(expireClock(len(c.ch), lastAt, now))...)
			for id, at := range seen {
				if now.Sub(at) > seenFor {
					delete(seen, id)
				}
			}
			if !now.Before(retryAt) {
				flush(ctx)
			}
		}
	}
}

// DefaultFinalWait bounds the last write at shutdown when no deadline was
// set with SetFinalDeadline.
const DefaultFinalWait = 10 * time.Second

// finalBatch is the batch size of the last write, so that what fits before
// the deadline is stored even when the whole backlog does not fit.
const finalBatch = 500

// finalReserve is kept back from the deadline to store the count of
// events that did not fit.
const finalReserve = 500 * time.Millisecond

// SetFinalDeadline sets when the last write at shutdown must end. Call it
// before cancelling Run's context.
func (c *Capture) SetFinalDeadline(t time.Time) { c.finalBy.Store(&t) }

// finalFlush writes buf in batches until the deadline, then stores the
// number of events that did not fit as a collector.messages_dropped event
// and logs it.
func (c *Capture) finalFlush(ctx context.Context, buf []Event) {
	deadline := c.now().Add(DefaultFinalWait)
	if p := c.finalBy.Load(); p != nil {
		deadline = *p
	}
	wctx, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
	defer cancel()
	for len(buf) > 0 && time.Until(deadline) > finalReserve {
		n := min(len(buf), finalBatch)
		bctx, bcancel := context.WithDeadline(wctx, deadline.Add(-finalReserve))
		err := c.o.Writer.WriteEvents(bctx, buf[:n])
		bcancel()
		if err != nil {
			c.o.Log.Error("events not stored at shutdown", "count", len(buf), "err", err)
			break
		}
		buf = buf[n:]
	}
	if len(buf) == 0 {
		return
	}
	c.o.Log.Error("events dropped at shutdown: not stored before the shutdown deadline", "count", len(buf))
	if err := c.o.Writer.WriteEvents(wctx, []Event{droppedEvent(int64(len(buf)), c.now())}); err != nil {
		c.o.Log.Error("count of dropped events not stored", "count", len(buf), "err", err)
	}
}

// expireClock is the time held actions expire by: while messages are
// queued, the receive time of the last processed one, so that an audit
// still waiting in the queue can match its action; otherwise now.
func expireClock(queued int, lastAt, now time.Time) time.Time {
	if queued > 0 && !lastAt.IsZero() {
		return lastAt
	}
	return now
}

// handle decodes one message and returns the events to store now. seen
// holds the payload IDs already handled, with their receive times.
func (c *Capture) handle(cor *correlator, seen map[string]time.Time, in incoming) []Event {
	if in.event != nil {
		return []Event{*in.event}
	}
	var ev Event
	var err error
	if in.sys || strings.HasPrefix(in.subject, "$SYS.") {
		ev, err = DecodeSys(in.subject, in.data, in.at)
	} else {
		ev, err = DecodeJS(in.account, in.subject, in.data, in.at)
	}
	if err != nil {
		c.o.Log.Warn("undecodable event", "subject", in.subject, "err", err)
		return nil
	}
	if ev.id != "" {
		if _, dup := seen[ev.id]; dup {
			return nil
		}
		seen[ev.id] = in.at
	}
	out := cor.add(in.account, ev, in.data, in.at)
	if !c.o.APIAudit {
		kept := out[:0]
		for _, e := range out {
			if e.Kind != KindAPIAudit {
				kept = append(kept, e)
			}
		}
		out = kept
	}
	return out
}

func droppedEvent(n int64, at time.Time) Event {
	at = at.UTC()
	return Event{Time: at, ReceivedAt: at, Source: SourceCollector, Kind: "collector.messages_dropped",
		Reason:  "queue full or write failed",
		Summary: fmt.Sprintf("collector dropped %d event messages: the queue was full or writes kept failing", n),
		Raw:     mustJSON(map[string]int64{"dropped": n})}
}

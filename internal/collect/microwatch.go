package collect

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// microInboxMark ends the collector's own reply inboxes, so that a refused
// subscription to one is known for the collector's.
const microInboxMark = ".srv"

// lookupClaimFor is how long after an account lookup is sent a refusal of
// $SYS.REQ.USER.INFO is taken for the collector's.
const lookupClaimFor = 5 * time.Second

// microWatchKeep is the most refusals a watch keeps.
const microWatchKeep = 64

// microWatch takes the permissions violations of one account connection
// that answer the collector's own requests: $SRV.INFO and $SRV.STATS, its
// reply inboxes, and its account lookup while one is in flight. Those
// never reach the connection's own error handler, which would record them
// as subscription errors or log them every epoch; every other error does.
type microWatch struct {
	mu sync.Mutex
	// errs are the refusals taken, newest last; seq counts every one ever
	// taken, so errs[i] has the number seq-len(errs)+i.
	errs []error
	seq  int
	// wake is closed and replaced when a refusal is taken.
	wake chan struct{}
	// lookupUntil is when a refused account lookup stops being the
	// collector's.
	lookupUntil time.Time
}

func newMicroWatch() *microWatch { return &microWatch{wake: make(chan struct{})} }

// install puts the watch in front of nc's error handler.
func (w *microWatch) install(nc *nats.Conn) {
	next := nc.ErrorHandler()
	nc.SetErrorHandler(func(c *nats.Conn, sub *nats.Subscription, err error) {
		if w.take(err) {
			return
		}
		if next != nil {
			next(c, sub, err)
		}
	})
}

// take records err when it refuses one of the collector's requests and
// reports whether it did.
func (w *microWatch) take(err error) bool {
	if !errors.Is(err, nats.ErrPermissionViolation) {
		return false
	}
	text := err.Error()
	w.mu.Lock()
	defer w.mu.Unlock()
	ours := strings.Contains(text, `"`+MicroInfo.Subject()+`"`) || strings.Contains(text, `"`+MicroStats.Subject()+`"`) ||
		(strings.Contains(text, `"`+nats.InboxPrefix) && strings.Contains(text, microInboxMark+`.*"`)) ||
		(strings.Contains(text, `"`+userInfoSubject+`"`) && time.Now().Before(w.lookupUntil))
	if !ours {
		return false
	}
	w.errs = append(w.errs, err)
	if len(w.errs) > microWatchKeep {
		w.errs = w.errs[len(w.errs)-microWatchKeep:]
	}
	w.seq++
	close(w.wake)
	w.wake = make(chan struct{})
	return true
}

// lookingUp marks an account lookup sent now.
func (w *microWatch) lookingUp() {
	w.mu.Lock()
	w.lookupUntil = time.Now().Add(lookupClaimFor)
	w.mu.Unlock()
}

// mark returns the number of refusals taken so far and a channel closed
// when the next is.
func (w *microWatch) mark() (int, <-chan struct{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seq, w.wake
}

// naming returns the first refusal taken after the mark from that names
// subject, or nil.
func (w *microWatch) naming(from int, subject string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	first := w.seq - len(w.errs)
	for i, err := range w.errs {
		if first+i >= from && strings.Contains(err.Error(), `"`+subject+`"`) {
			return err
		}
	}
	return nil
}

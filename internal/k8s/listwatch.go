package k8s

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/shadone/nats-lens/internal/events"
)

// Limits of a list.
const (
	// listPage is the page size of a list.
	listPage = 500
	// maxListPages bounds a (re)list; what is beyond is left out with a
	// warning, so a huge namespace cannot hold the watcher in a list.
	maxListPages = 20
)

// Waits between attempts after a failed list or watch.
const (
	minBackoff = time.Second
	maxBackoff = time.Minute
	// minWatchLife is the shortest life of a watch that ends without an
	// error before it counts as a failure.
	minWatchLife = time.Second
)

// source is one list and watch: a resource in a namespace.
type source struct {
	// kind is pods, nodes or events; ns is the namespace, empty for nodes.
	kind, ns string
	// name describes the source in logs and gap events, for example
	// "pods in example".
	name string
	// list returns one page of objects, the list's resource version and
	// the continue token.
	list  func(ctx context.Context, opts metav1.ListOptions) ([]runtime.Object, string, string, error)
	watch func(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
}

// update is what a source hands the tracker: a (re)list or a watch event.
type update struct {
	src     *source
	listed  []runtime.Object
	isList  bool
	initial bool
	ev      watch.Event
}

// loop lists and watches one source until ctx ends. It watches from the
// resource version of the last object it saw, so a watch that ends (the
// server closes it every few minutes) resumes without a gap; only a
// resource version the server no longer has (410 Gone) needs a relist.
// A source that keeps failing for gapAfter records a
// collector.k8s_not_watching event, and its recovery a collector.k8s_gap
// with the span in which changes may have been missed.
type loop struct {
	src      *source
	out      chan<- update
	record   func(events.Event)
	gapAfter time.Duration
	now      func() time.Time
	log      *slog.Logger
	// sleep waits d or until ctx ends; a field so tests need not wait.
	sleep func(ctx context.Context, d time.Duration)

	failingSince time.Time
	notified     bool
	lastErr      error
	backoff      time.Duration
}

func (l *loop) run(ctx context.Context) {
	rv := ""
	initial, needList := true, true
	l.backoff = minBackoff
	for ctx.Err() == nil {
		if needList {
			items, listRV, err := l.listAll(ctx)
			if err != nil {
				l.fail(ctx, "list", err)
				continue
			}
			l.recovered()
			if !l.send(ctx, update{src: l.src, listed: items, isList: true, initial: initial}) {
				return
			}
			initial, needList, rv = false, false, listRV
		}
		timeout := int64(300 + rand.IntN(300))
		w, err := l.src.watch(ctx, metav1.ListOptions{ResourceVersion: rv, AllowWatchBookmarks: true, TimeoutSeconds: &timeout})
		if err != nil {
			if isGone(err) {
				needList = true
				continue
			}
			l.fail(ctx, "watch", err)
			continue
		}
		// An open watch ends a failure: a quiet namespace may then send
		// nothing for minutes, which is no gap.
		l.recovered()
		opened := time.Now()
		next, err := l.consume(ctx, w, rv)
		w.Stop()
		// A watch the server ends at once without an error (a proxy that
		// closes it) would otherwise be reopened in a tight loop: it counts
		// as a failure, and the next one opens a second after this one at
		// the soonest. Its life is measured on the wall clock.
		if lived := time.Since(opened); err == nil && ctx.Err() == nil && lived < minWatchLife {
			err = fmt.Errorf("watch closed by the server after %s", lived.Round(time.Millisecond))
			sleepCtx(ctx, minWatchLife-lived)
		}
		switch {
		case ctx.Err() != nil:
			return
		case isGone(err):
			l.log.Info("kubernetes watch expired; listing again", "source", l.src.name)
			needList = true
		case err != nil:
			rv = next
			l.fail(ctx, "watch", err)
		default:
			rv = next
			l.recovered()
		}
	}
}

// consume forwards the watch's events until it ends, and returns the
// resource version to resume from.
func (l *loop) consume(ctx context.Context, w watch.Interface, rv string) (string, error) {
	for {
		select {
		case <-ctx.Done():
			return rv, ctx.Err()
		case ev, ok := <-w.ResultChan():
			if !ok {
				return rv, nil
			}
			switch ev.Type {
			case watch.Error:
				return rv, apierrors.FromObject(ev.Object)
			case watch.Bookmark:
				if a, err := meta.Accessor(ev.Object); err == nil && a.GetResourceVersion() != "" {
					rv = a.GetResourceVersion()
				}
				continue
			}
			if a, err := meta.Accessor(ev.Object); err == nil && a.GetResourceVersion() != "" {
				rv = a.GetResourceVersion()
			}
			l.recovered()
			if !l.send(ctx, update{src: l.src, ev: ev}) {
				return rv, ctx.Err()
			}
		}
	}
}

// listAll lists every page, up to maxListPages.
func (l *loop) listAll(ctx context.Context) ([]runtime.Object, string, error) {
	var all []runtime.Object
	var rv, cont string
	for page := 0; ; page++ {
		if page == maxListPages {
			l.log.Warn("kubernetes list truncated", "source", l.src.name, "objects", len(all))
			return all, rv, nil
		}
		items, pageRV, next, err := l.src.list(ctx, metav1.ListOptions{Limit: listPage, Continue: cont})
		if err != nil {
			return nil, "", err
		}
		if page == 0 {
			rv = pageRV
		}
		all = append(all, items...)
		if next == "" {
			return all, rv, nil
		}
		cont = next
	}
}

func (l *loop) send(ctx context.Context, u update) bool {
	select {
	case l.out <- u:
		return true
	case <-ctx.Done():
		return false
	}
}

// fail logs a failed list or watch, records it once it lasted gapAfter,
// and waits before the next attempt.
func (l *loop) fail(ctx context.Context, op string, err error) {
	if ctx.Err() != nil {
		return
	}
	now := l.now()
	if l.failingSince.IsZero() {
		l.failingSince = now
	}
	l.lastErr = err
	l.log.Warn("kubernetes "+op+" failed", "source", l.src.name, "err", err, "retry_in", l.backoff)
	if !l.notified && now.Sub(l.failingSince) >= l.gapAfter {
		l.notified = true
		l.record(collectorEvent(now, KindNotWatching, err.Error(),
			fmt.Sprintf("kubernetes %s not watched since %s: %v", l.src.name, l.failingSince.UTC().Format(time.RFC3339), err),
			map[string]any{"source": l.src.name, "since": l.failingSince.UTC(), "error": err.Error(), "status": statusCode(err)}))
	}
	l.sleep(ctx, l.backoff)
	l.backoff = min(2*l.backoff, maxBackoff)
}

// recovered records the gap of a failure that lasted gapAfter.
func (l *loop) recovered() {
	l.backoff = minBackoff
	if l.failingSince.IsZero() {
		return
	}
	now := l.now()
	from := l.failingSince
	l.failingSince = time.Time{}
	notified := l.notified
	l.notified = false
	if !notified && now.Sub(from) < l.gapAfter {
		return
	}
	gap := now.Sub(from).Round(time.Second)
	msg := ""
	if l.lastErr != nil {
		msg = l.lastErr.Error()
	}
	ev := collectorEvent(from, KindGap, msg,
		fmt.Sprintf("kubernetes %s not watched for %s, from %s to %s", l.src.name, gap, from.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339)),
		map[string]any{"source": l.src.name, "from": from.UTC(), "to": now.UTC(), "seconds": int64(gap / time.Second), "error": msg})
	ev.ReceivedAt = now.UTC()
	l.record(ev)
	l.log.Info("kubernetes watch restored", "source", l.src.name, "gap", gap)
}

func collectorEvent(at time.Time, kind, reason, summary string, raw map[string]any) events.Event {
	ev := events.Event{Time: at.UTC(), ReceivedAt: at.UTC(), Source: events.SourceCollector, Kind: kind, Reason: reason, Summary: summary}
	ev.Raw = mustJSON(raw)
	return ev
}

// isGone reports a resource version the API server no longer has.
func isGone(err error) bool {
	return apierrors.IsResourceExpired(err) || apierrors.IsGone(err)
}

func statusCode(err error) int32 {
	var s apierrors.APIStatus
	if errors.As(err, &s) {
		return s.Status().Code
	}
	return 0
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

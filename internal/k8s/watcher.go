package k8s

import (
	"context"
	"log/slog"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"

	"github.com/shadone/nats-lens/internal/events"
)

// Sink receives the events the watcher records; the event capture's
// Record writes them in its batches.
type Sink interface {
	Record(events.Event)
}

// Options configure a Watcher.
type Options struct {
	Client kubernetes.Interface
	// Namespaces are watched for Events and NATS pods.
	Namespaces []string
	// NATSSelector selects the NATS server pods.
	NATSSelector string
	// ServerName maps a NATS pod to its server name; {pod} and
	// {namespace} are replaced.
	ServerName string
	// NodeEventsNamespace holds the Events of nodes.
	NodeEventsNamespace string
	// Nodes turns the node watch and node Events on.
	Nodes bool
	// PoolNodes keeps the changes of every node of a pool that held a NATS
	// node.
	PoolNodes bool
	// GapAfter is how long a watch may fail before it is recorded.
	GapAfter time.Duration
	Sink     Sink
	// Seen holds the counts of Events already stored, by UID, so that a
	// restart does not store them again.
	Seen map[string]int64
	// Prior is the stored state of pods and nodes; the first lists record
	// what changed while natslens was down, and a gap since Prior.Last.
	Prior Prior
	Log   *slog.Logger
	// Now returns the time; nil means the wall clock.
	Now func() time.Time
}

// podsFirstWait is how long the node and Event watches wait for the first
// lists of the NATS pods, which tell them which pods and nodes are NATS'.
const podsFirstWait = 10 * time.Second

// expireEvery is how often the tracker forgets old state.
const expireEvery = 10 * time.Minute

// Watcher lists and watches the sources and records their changes.
type Watcher struct {
	o     Options
	t     *tracker
	sleep func(ctx context.Context, d time.Duration)
}

// New returns a Watcher; call Run to start it.
func New(o Options) *Watcher {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.GapAfter <= 0 {
		o.GapAfter = 30 * time.Second
	}
	if o.ServerName == "" {
		o.ServerName = "{pod}"
	}
	if o.NodeEventsNamespace == "" {
		o.NodeEventsNamespace = "default"
	}
	t := newTracker(serverNameFunc(o.ServerName), o.Now, o.Sink.Record, o.Seen)
	t.poolNodes = o.PoolNodes
	t.prior = o.Prior
	return &Watcher{o: o, t: t, sleep: sleepCtx}
}

// Run watches until ctx ends. The NATS pods are listed first, so that
// the first Events and nodes are matched to them.
func (w *Watcher) Run(ctx context.Context) error {
	updates := make(chan update, 256)
	var wg sync.WaitGroup
	start := func(s *source) {
		l := &loop{src: s, out: updates, record: w.o.Sink.Record, gapAfter: w.o.GapAfter, now: w.o.Now,
			log: w.o.Log, sleep: w.sleep}
		wg.Go(func() { l.run(ctx) })
	}
	pending := 0
	for _, ns := range w.o.Namespaces {
		start(w.podSource(ns))
		pending++
	}
	started := false
	startOthers := func() {
		if started {
			return
		}
		started = true
		for _, s := range w.otherSources() {
			start(s)
		}
	}
	wait := time.NewTimer(podsFirstWait)
	defer wait.Stop()
	expire := time.NewTicker(expireEvery)
	defer expire.Stop()
	if pending == 0 {
		startOthers()
	}
	if ev, ok := startGap(w.o.Prior, w.o.Now(), w.o.GapAfter); ok {
		w.o.Sink.Record(ev)
	}
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil
		case <-wait.C:
			startOthers()
		case <-expire.C:
			w.t.expire()
		case u := <-updates:
			w.apply(u)
			if u.isList && u.initial && u.src.kind == "pods" {
				if pending--; pending == 0 {
					startOthers()
				}
			}
		}
	}
}

// otherSources are the Event watches and the node watch.
func (w *Watcher) otherSources() []*source {
	var out []*source
	nodeNS := false
	for _, ns := range w.o.Namespaces {
		out = append(out, w.eventSource(ns, ""))
		nodeNS = nodeNS || ns == w.o.NodeEventsNamespace
	}
	if w.o.Nodes {
		out = append(out, w.nodeSource())
		if !nodeNS {
			out = append(out, w.eventSource(w.o.NodeEventsNamespace, "regarding.kind=Node"))
		}
	}
	return out
}

// apply hands one update to the tracker.
func (w *Watcher) apply(u update) {
	t := w.t
	if u.isList {
		switch u.src.kind {
		case "pods":
			pods := make([]*corev1.Pod, 0, len(u.listed))
			for _, o := range u.listed {
				if p, ok := o.(*corev1.Pod); ok {
					pods = append(pods, p)
				}
			}
			t.podsListed(u.src.ns, pods, u.initial)
		case "nodes":
			nodes := make([]*corev1.Node, 0, len(u.listed))
			for _, o := range u.listed {
				if n, ok := o.(*corev1.Node); ok {
					nodes = append(nodes, n)
				}
			}
			t.nodesListed(nodes, u.initial)
		case "events":
			for _, o := range u.listed {
				if e, ok := o.(*eventsv1.Event); ok {
					t.eventObserved(e)
				}
			}
		}
		return
	}
	switch o := u.ev.Object.(type) {
	case *corev1.Pod:
		if u.ev.Type == watch.Deleted {
			t.podDeleted(o)
		} else {
			t.podChanged(o)
		}
	case *corev1.Node:
		if u.ev.Type == watch.Deleted {
			s := nodeStateOf(o)
			if old := t.nodes[o.Name]; old != nil {
				s.natsAt = old.natsAt
			}
			t.nodeGone(s)
		} else {
			t.nodeChanged(o)
		}
	case *eventsv1.Event:
		if u.ev.Type != watch.Deleted {
			t.eventObserved(o)
		}
	}
}

func (w *Watcher) podSource(ns string) *source {
	pods := w.o.Client.CoreV1().Pods(ns)
	sel := w.o.NATSSelector
	return &source{
		kind: "pods", ns: ns, name: "pods " + sel + " in " + ns,
		list: func(ctx context.Context, opts metav1.ListOptions) ([]runtime.Object, string, string, error) {
			opts.LabelSelector = sel
			l, err := pods.List(ctx, opts)
			if err != nil {
				return nil, "", "", err
			}
			out := make([]runtime.Object, len(l.Items))
			for i := range l.Items {
				out[i] = &l.Items[i]
			}
			return out, l.ResourceVersion, l.Continue, nil
		},
		watch: func(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
			opts.LabelSelector = sel
			return pods.Watch(ctx, opts)
		},
	}
}

func (w *Watcher) nodeSource() *source {
	nodes := w.o.Client.CoreV1().Nodes()
	return &source{
		kind: "nodes", name: "nodes",
		list: func(ctx context.Context, opts metav1.ListOptions) ([]runtime.Object, string, string, error) {
			l, err := nodes.List(ctx, opts)
			if err != nil {
				return nil, "", "", err
			}
			out := make([]runtime.Object, len(l.Items))
			for i := range l.Items {
				out[i] = &l.Items[i]
			}
			return out, l.ResourceVersion, l.Continue, nil
		},
		watch: nodes.Watch,
	}
}

// eventSource watches events.k8s.io/v1 Events. The core/v1 Events API
// serves the same objects, so watching both would only double the
// traffic.
func (w *Watcher) eventSource(ns, fieldSelector string) *source {
	evs := w.o.Client.EventsV1().Events(ns)
	name := "events in " + ns
	if fieldSelector != "" {
		name += " (" + fieldSelector + ")"
	}
	return &source{
		kind: "events", ns: ns, name: name,
		list: func(ctx context.Context, opts metav1.ListOptions) ([]runtime.Object, string, string, error) {
			opts.FieldSelector = fieldSelector
			l, err := evs.List(ctx, opts)
			if err != nil {
				return nil, "", "", err
			}
			out := make([]runtime.Object, len(l.Items))
			for i := range l.Items {
				out[i] = &l.Items[i]
			}
			return out, l.ResourceVersion, l.Continue, nil
		},
		watch: func(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
			opts.FieldSelector = fieldSelector
			return evs.Watch(ctx, opts)
		},
	}
}

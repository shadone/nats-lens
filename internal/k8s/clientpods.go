package k8s

import (
	"context"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"

	"github.com/shadone/nats-lens/internal/events"
)

// ClientPod is what the client pod watch keeps of a pod: enough to say
// where a connection from one of its addresses runs.
type ClientPod struct {
	UID, Namespace, Name, Node string
	// WorkloadKind and Workload name the pod's owner (see workloadOf).
	WorkloadKind, Workload string
	// AppName is the app.kubernetes.io/name label; empty without one.
	AppName     string
	IPs         []string
	HostNetwork bool
	// StartedAt is status.startTime; zero when not set.
	StartedAt time.Time
}

// ClientPodRelist is a (re)list of one namespace, empty for every
// namespace, with the UIDs of the live pods it listed.
type ClientPodRelist struct {
	Namespace string
	Listed    []string
}

// ClientPodBatch is what the watch writes at once: the pods that changed,
// the pods that released their addresses, the lists, and whether the
// live pods' last sighting is refreshed.
type ClientPodBatch struct {
	At      time.Time
	Seen    []ClientPod
	Ended   map[string]time.Time
	Relists []ClientPodRelist
	Touch   bool
}

// ClientPodOptions configure a ClientPodWatcher.
type ClientPodOptions struct {
	Client kubernetes.Interface
	// Namespaces are watched for client pods; AllNamespaces watches every
	// namespace with one cluster-wide watch instead.
	Namespaces    []string
	AllNamespaces bool
	// Write stores a batch; a failed batch is merged into the next.
	Write func(context.Context, ClientPodBatch) error
	// Record receives the collector events of a watch that fails for
	// GapAfter and of its recovery.
	Record   func(events.Event)
	GapAfter time.Duration
	Log      *slog.Logger
	// Now returns the time; nil means the wall clock.
	Now func() time.Time
	// FlushEvery is how often changes are written (15 s by default), and
	// TouchEvery how often the live pods' last sighting is refreshed
	// (5 minutes by default).
	FlushEvery, TouchEvery time.Duration
}

// maxClientPods bounds the pods kept in memory; pods beyond it are not
// matched, with a warning.
const maxClientPods = 50_000

// clientPodWarnEvery spaces the repeated failure logs of a client pod
// watch: its first failure explains itself once, and it is optional.
const clientPodWarnEvery = time.Hour

// ClientPodWatcher lists and watches the pods of NATS clients in the
// configured namespaces and writes their addresses, so that connections
// can be matched to the pods they come from. It only reads from the API
// server, and only Pods.
type ClientPodWatcher struct {
	o     ClientPodOptions
	sleep func(ctx context.Context, d time.Duration)

	// live are the pods kept, by UID; the main goroutine owns it.
	live    map[string]*ClientPod
	pending ClientPodBatch
	dirty   map[string]bool
	full    bool
	// warned holds the sources whose permission failure was explained.
	warned sync.Map
}

// NewClientPodWatcher returns a ClientPodWatcher; call Run to start it.
func NewClientPodWatcher(o ClientPodOptions) *ClientPodWatcher {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.GapAfter <= 0 {
		o.GapAfter = 30 * time.Second
	}
	if o.FlushEvery <= 0 {
		o.FlushEvery = 15 * time.Second
	}
	if o.TouchEvery <= 0 {
		o.TouchEvery = 5 * time.Minute
	}
	return &ClientPodWatcher{o: o, sleep: sleepCtx, live: map[string]*ClientPod{}, dirty: map[string]bool{},
		pending: ClientPodBatch{Ended: map[string]time.Time{}}}
}

// Run watches until ctx ends, then writes what is pending.
func (w *ClientPodWatcher) Run(ctx context.Context) error {
	updates := make(chan update, 256)
	var wg sync.WaitGroup
	namespaces := w.o.Namespaces
	if w.o.AllNamespaces {
		namespaces = []string{""}
	}
	for _, ns := range namespaces {
		l := &loop{src: w.source(ns), out: updates, record: w.o.Record, gapAfter: w.o.GapAfter, now: w.o.Now,
			log: w.o.Log, sleep: w.sleep, warnEvery: clientPodWarnEvery}
		wg.Go(func() { l.run(ctx) })
	}
	flush := time.NewTicker(w.o.FlushEvery)
	defer flush.Stop()
	lastTouch := w.o.Now()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			// The store outlives the watch: write the last changes.
			fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			w.flush(fctx, false)
			cancel()
			return nil
		case u := <-updates:
			w.apply(u)
		case <-flush.C:
			touch := w.o.Now().Sub(lastTouch) >= w.o.TouchEvery
			if w.flush(ctx, touch) && touch {
				lastTouch = w.o.Now()
			}
		}
	}
}

// apply takes one list or watch event.
func (w *ClientPodWatcher) apply(u update) {
	now := w.o.Now()
	if u.isList {
		var listed []string
		present := map[string]bool{}
		for _, o := range u.listed {
			p, ok := o.(*corev1.Pod)
			if !ok {
				continue
			}
			if ended(p) {
				if _, live := w.live[string(p.UID)]; live {
					w.pending.Ended[string(p.UID)] = now
				}
				continue
			}
			listed = append(listed, string(p.UID))
			present[string(p.UID)] = true
			w.changed(p)
		}
		for uid, p := range w.live {
			if !present[uid] && (u.src.ns == "" || p.Namespace == u.src.ns) {
				delete(w.live, uid)
				delete(w.dirty, uid)
			}
		}
		w.pending.Relists = append(w.pending.Relists, ClientPodRelist{Namespace: u.src.ns, Listed: listed})
		return
	}
	p, ok := u.ev.Object.(*corev1.Pod)
	if !ok {
		return
	}
	uid := string(p.UID)
	if u.ev.Type == watch.Deleted || ended(p) {
		if _, live := w.live[uid]; live || w.dirty[uid] {
			delete(w.live, uid)
			delete(w.dirty, uid)
			w.pending.Ended[uid] = now
		}
		return
	}
	w.changed(p)
}

// changed keeps a live pod and marks it to be written when what is kept
// of it changed. A pod without an address yet is not kept.
func (w *ClientPodWatcher) changed(p *corev1.Pod) {
	c := clientPodOf(p)
	if len(c.IPs) == 0 {
		return
	}
	old, ok := w.live[c.UID]
	if !ok && len(w.live) >= maxClientPods {
		if !w.full {
			w.full = true
			w.o.Log.Warn("kubernetes client pods: more pods than kept; the others are not matched to connections", "max", maxClientPods)
		}
		return
	}
	if ok && old.Node == c.Node && old.WorkloadKind == c.WorkloadKind && old.Workload == c.Workload && old.AppName == c.AppName &&
		old.HostNetwork == c.HostNetwork && slices.Equal(old.IPs, c.IPs) {
		return
	}
	w.live[c.UID] = &c
	w.dirty[c.UID] = true
}

// flush writes the pending batch; it reports whether the write
// succeeded. A failed write is kept and merged into the next one.
func (w *ClientPodWatcher) flush(ctx context.Context, touch bool) bool {
	b := w.pending
	b.At, b.Touch = w.o.Now(), touch
	for uid := range w.dirty {
		if p := w.live[uid]; p != nil {
			b.Seen = append(b.Seen, *p)
		}
	}
	slices.SortFunc(b.Seen, func(x, y ClientPod) int { return strings.Compare(x.UID, y.UID) })
	if len(b.Seen) == 0 && len(b.Ended) == 0 && len(b.Relists) == 0 && !b.Touch {
		return true
	}
	if err := w.o.Write(ctx, b); err != nil {
		w.o.Log.Warn("kubernetes client pods not written; trying again", "err", err)
		return false
	}
	w.pending = ClientPodBatch{Ended: map[string]time.Time{}}
	w.dirty = map[string]bool{}
	return true
}

// ended reports a pod whose containers ended: it released its address.
func ended(p *corev1.Pod) bool {
	return p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed
}

func clientPodOf(p *corev1.Pod) ClientPod {
	c := ClientPod{UID: string(p.UID), Namespace: p.Namespace, Name: p.Name, Node: p.Spec.NodeName, HostNetwork: p.Spec.HostNetwork,
		AppName: p.Labels["app.kubernetes.io/name"]}
	c.WorkloadKind, c.Workload = workloadOf(p)
	for _, ip := range p.Status.PodIPs {
		if ip.IP != "" && !slices.Contains(c.IPs, ip.IP) {
			c.IPs = append(c.IPs, ip.IP)
		}
	}
	if len(c.IPs) == 0 && p.Status.PodIP != "" {
		c.IPs = []string{p.Status.PodIP}
	}
	if p.Status.StartTime != nil {
		c.StartedAt = p.Status.StartTime.UTC()
	}
	return c
}

// cronJobName matches the name the CronJob controller gives a Job: the
// CronJob's name and the scheduled time in minutes since 1970.
var cronJobName = regexp.MustCompile(`^(.+)-(\d{8,})$`)

// workloadOf names the workload that owns a pod from its controller
// reference alone, without reading the owner: a ReplicaSet whose name is
// a Deployment's and the pod-template-hash label is that Deployment, a
// Job named as the CronJob controller names them is that CronJob, any
// other controller is itself, and a pod without one is a bare Pod.
func workloadOf(p *corev1.Pod) (kind, name string) {
	ref := metav1.GetControllerOf(p)
	if ref == nil {
		return "Pod", p.Name
	}
	switch ref.Kind {
	case "ReplicaSet":
		if h := p.Labels["pod-template-hash"]; h != "" && strings.HasSuffix(ref.Name, "-"+h) && len(ref.Name) > len(h)+1 {
			return "Deployment", strings.TrimSuffix(ref.Name, "-"+h)
		}
	case "Job":
		if m := cronJobName.FindStringSubmatch(ref.Name); m != nil {
			return "CronJob", m[1]
		}
	}
	return ref.Kind, ref.Name
}

// source lists and watches every pod of namespace ns ("" for all).
func (w *ClientPodWatcher) source(ns string) *source {
	pods := w.o.Client.CoreV1().Pods(ns)
	where := "in " + ns
	if ns == "" {
		where = "in all namespaces"
	}
	s := &source{kind: "clientpods", ns: ns, name: "client pods " + where}
	s.list = func(ctx context.Context, opts metav1.ListOptions) ([]runtime.Object, string, string, error) {
		l, err := pods.List(ctx, opts)
		if err != nil {
			w.explain(s.name, ns, err)
			return nil, "", "", err
		}
		out := make([]runtime.Object, len(l.Items))
		for i := range l.Items {
			out[i] = &l.Items[i]
		}
		return out, l.ResourceVersion, l.Continue, nil
	}
	s.watch = func(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
		wi, err := pods.Watch(ctx, opts)
		if err != nil {
			w.explain(s.name, ns, err)
		}
		return wi, err
	}
	return s
}

// explain logs once per source why a forbidden request failed and what
// to grant.
func (w *ClientPodWatcher) explain(name, ns string, err error) {
	if !apierrors.IsForbidden(err) {
		return
	}
	if _, done := w.warned.LoadOrStore(name, true); done {
		return
	}
	grant := "a Role that may list and watch pods in " + ns + " (k8s/components/client-pods)"
	if ns == "" {
		grant = "a ClusterRole that may list and watch pods (k8s/components/client-pods-all)"
	}
	w.o.Log.Warn("kubernetes.client_pod_watch: natslens may not watch "+name+"; Runs on stays incomplete, everything else goes on. Grant "+
		grant+", or remove the namespace from kubernetes.client_pod_watch", "source", name, "retry", "with backoff")
}

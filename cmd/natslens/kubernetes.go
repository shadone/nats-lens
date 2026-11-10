package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/events"
	"github.com/shadone/nats-lens/internal/k8s"
	"github.com/shadone/nats-lens/internal/store"
)

// seenWindow is how far back the stored Event counts are read at start;
// the API server keeps an Event for an hour by default.
const seenWindow = 3 * time.Hour

// priorWindow is how far back the last stored state of pods and nodes is
// read, to record at start what changed while natslens was down.
const priorWindow = 24 * time.Hour

// newK8sClient makes the Kubernetes client; a variable so that tests can
// hand in a fake clientset.
var newK8sClient = k8s.NewClient

// startKubernetes starts the Kubernetes watcher, which writes through
// the capture. The returned stop ends it and waits; call it before the
// capture stops, so that its last events are written.
func startKubernetes(ctx context.Context, cfg config.Kubernetes, st *store.Store, capture *events.Capture, log *slog.Logger) (func(), error) {
	client, own, err := newK8sClient(cfg, "natslens/"+version)
	if err != nil {
		return nil, err
	}
	namespaces, err := k8s.Namespaces(cfg, own)
	if err != nil {
		return nil, err
	}
	seen, err := st.K8sEventCounts(ctx, time.Now().Add(-seenWindow))
	if err != nil {
		return nil, fmt.Errorf("read stored kubernetes events: %w", err)
	}
	pods, nodes, last, err := st.K8sLastState(ctx, time.Now().Add(-priorWindow))
	if err != nil {
		return nil, fmt.Errorf("read stored kubernetes state: %w", err)
	}
	prior := k8s.Prior{Pods: priorStates(pods), Nodes: priorStates(nodes), Last: last}
	log = log.With("component", "kubernetes")
	log.Info("watching kubernetes", "namespaces", namespaces, "nats_selector", cfg.NATSSelector, "nodes", cfg.Nodes)
	w := k8s.New(k8s.Options{
		Client: client, Namespaces: namespaces, NATSSelector: cfg.NATSSelector, ServerName: cfg.ServerName,
		NodeEventsNamespace: cfg.NodeEventsNamespace, Nodes: cfg.Nodes, PoolNodes: cfg.PoolNodes, GapAfter: cfg.GapAfter.D(),
		Sink: capture, Seen: seen, Prior: prior, Log: log,
	})
	wctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Go(func() { _ = w.Run(wctx) })
	// The client pod watch is opt-in: without kubernetes.client_pod_watch
	// no request for client pods is made.
	if cw := cfg.ClientPodWatch; cw.On() {
		log.Info("watching client pods", "namespaces", cw.Namespaces, "all_namespaces", cw.AllNamespaces)
		cp := k8s.NewClientPodWatcher(k8s.ClientPodOptions{
			Client: client, Namespaces: cw.Namespaces, AllNamespaces: cw.AllNamespaces, Record: capture.Record,
			GapAfter: cfg.GapAfter.D(), Log: log.With("watch", "client_pods"),
			Write: func(ctx context.Context, b k8s.ClientPodBatch) error {
				return st.WriteClientPods(ctx, storeClientPods(b))
			},
		})
		wg.Go(func() { _ = cp.Run(wctx) })
	}
	return func() {
		cancel()
		wg.Wait()
	}, nil
}

// storeClientPods converts the watch's batch to the store's.
func storeClientPods(b k8s.ClientPodBatch) store.ClientPodBatch {
	out := store.ClientPodBatch{At: b.At, Ended: b.Ended, Touch: b.Touch}
	for _, p := range b.Seen {
		out.Seen = append(out.Seen, store.ClientPod{UID: p.UID, Namespace: p.Namespace, Name: p.Name, Node: p.Node,
			WorkloadKind: p.WorkloadKind, Workload: p.Workload, AppName: p.AppName, IPs: p.IPs, HostNetwork: p.HostNetwork,
			StartedAt: p.StartedAt})
	}
	for _, r := range b.Relists {
		out.Relists = append(out.Relists, store.ClientPodRelist{Namespace: r.Namespace, Listed: r.Listed})
	}
	return out
}

func priorStates(in map[string]store.K8sObjectState) map[string]k8s.PriorState {
	out := make(map[string]k8s.PriorState, len(in))
	for k, v := range in {
		out[k] = k8s.PriorState{Flag: v.Flag, Deleted: v.Deleted}
	}
	return out
}

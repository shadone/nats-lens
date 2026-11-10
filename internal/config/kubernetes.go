package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Kubernetes configures the optional watcher of the Kubernetes objects
// around the NATS servers: Events in Namespaces, the NATS pods, and the
// nodes they run on. It only reads from the API server.
type Kubernetes struct {
	// Enabled turns the watcher on; it needs events.enabled, as it writes
	// through the event capture.
	Enabled bool `yaml:"enabled"`
	// Kubeconfig is a kubeconfig file for a run outside the cluster; empty
	// means the in-cluster service account.
	Kubeconfig string `yaml:"kubeconfig"`
	// Context is the kubeconfig context; empty means its current context.
	Context string `yaml:"context"`
	// Namespaces are watched for Events and for NATS pods; empty means the
	// namespace natslens runs in.
	Namespaces []string `yaml:"namespaces"`
	// NATSSelector is the label selector of the NATS server pods.
	NATSSelector string `yaml:"nats_selector"`
	// ServerName maps a NATS pod to its NATS server name: {pod} is the pod
	// name and {namespace} its namespace.
	ServerName string `yaml:"server_name"`
	// NodeEventsNamespace is where the API server files the Events of
	// nodes; Kubernetes uses default.
	NodeEventsNamespace string `yaml:"node_events_namespace"`
	// Nodes turns the node watch on; it needs a ClusterRole that may get,
	// list and watch nodes.
	Nodes bool `yaml:"nodes"`
	// PoolNodes keeps the changes of every node in a node pool that held a
	// NATS node within a day, not only the NATS nodes and the nodes added
	// during a drain. Off by default: on Autopilot the pools are the
	// autoscaler's and hold every workload.
	PoolNodes bool `yaml:"pool_nodes"`
	// GapAfter is how long a watch may fail before it is recorded as a
	// collector.k8s_not_watching event; its recovery records the gap.
	GapAfter Duration `yaml:"gap_after"`
	// ClientPods is a regular expression of the names of pods that are
	// NATS clients, besides those a connection name gives after "@"; their
	// failed scheduling, crash loops and OOM kills are drawn on the
	// timeline. Empty matches none.
	ClientPods string `yaml:"client_pods"`
	// ClientPodWatch turns on the watch of the pods of NATS clients, which
	// gives the Applications page where each application runs. Off unless
	// set: no request is made for client pods and no permission is needed.
	ClientPodWatch ClientPodWatch `yaml:"client_pod_watch"`
}

// ClientPodWatch lists where the pods of NATS clients are watched: in
// Namespaces (a Role in each suffices), or in every namespace with
// AllNamespaces (needs a ClusterRole). Neither set means no watch.
type ClientPodWatch struct {
	Namespaces    []string `yaml:"namespaces"`
	AllNamespaces bool     `yaml:"all_namespaces"`
}

// On reports whether client pods are watched.
func (c ClientPodWatch) On() bool { return c.AllNamespaces || len(c.Namespaces) > 0 }

// ClientPodsPattern returns ClientPods compiled; nil when empty or
// invalid (validation reports an invalid one).
func (k Kubernetes) ClientPodsPattern() *regexp.Regexp {
	if k.ClientPods == "" {
		return nil
	}
	re, err := regexp.Compile(k.ClientPods)
	if err != nil {
		return nil
	}
	return re
}

// DefaultNATSSelector selects the server pods of the official NATS Helm
// chart.
const DefaultNATSSelector = "app.kubernetes.io/name=nats,app.kubernetes.io/component=nats"

func defaultKubernetes() Kubernetes {
	return Kubernetes{
		NATSSelector:        DefaultNATSSelector,
		ServerName:          "{pod}",
		NodeEventsNamespace: "default",
		Nodes:               true,
		GapAfter:            Duration(30 * time.Second),
	}
}

func (k Kubernetes) validate(events Events) []error {
	var errs []error
	if _, err := regexp.Compile(k.ClientPods); err != nil {
		errs = append(errs, fmt.Errorf("kubernetes.client_pods: %w", err))
	}
	errs = append(errs, k.ClientPodWatch.validate(k.Enabled)...)
	if !k.Enabled {
		return errs
	}
	if !events.Enabled {
		errs = append(errs, errors.New("kubernetes.enabled needs events.enabled: Kubernetes events are written by the event capture"))
	}
	if strings.TrimSpace(k.NATSSelector) == "" {
		errs = append(errs, errors.New("kubernetes.nats_selector is required"))
	}
	if !strings.Contains(k.ServerName, "{pod}") {
		errs = append(errs, fmt.Errorf("kubernetes.server_name %q must contain {pod}", k.ServerName))
	}
	if k.NodeEventsNamespace == "" {
		errs = append(errs, errors.New("kubernetes.node_events_namespace is required"))
	}
	if k.GapAfter.D() <= 0 {
		errs = append(errs, errors.New("kubernetes.gap_after must be positive"))
	}
	for i, ns := range k.Namespaces {
		if strings.TrimSpace(ns) == "" {
			errs = append(errs, fmt.Errorf("kubernetes.namespaces[%d] is empty", i))
		}
	}
	return errs
}

func (c ClientPodWatch) validate(enabled bool) []error {
	var errs []error
	if !c.On() {
		return nil
	}
	if !enabled {
		errs = append(errs, errors.New("kubernetes.client_pod_watch needs kubernetes.enabled"))
	}
	if c.AllNamespaces && len(c.Namespaces) > 0 {
		errs = append(errs, errors.New("kubernetes.client_pod_watch: set namespaces or all_namespaces, not both"))
	}
	for i, ns := range c.Namespaces {
		if strings.TrimSpace(ns) == "" {
			errs = append(errs, fmt.Errorf("kubernetes.client_pod_watch.namespaces[%d] is empty", i))
		}
	}
	return errs
}

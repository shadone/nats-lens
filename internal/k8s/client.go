package k8s

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/shadone/nats-lens/internal/config"
)

// namespaceFile holds the namespace of the pod's service account.
const namespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// NewClient returns a client for the in-cluster service account, or for
// the kubeconfig file when one is set, and the namespace it runs in (the
// pod's, or the kubeconfig context's).
func NewClient(c config.Kubernetes, userAgent string) (kubernetes.Interface, string, error) {
	var rc *rest.Config
	var ns string
	if c.Kubeconfig == "" {
		var err error
		if rc, err = rest.InClusterConfig(); err != nil {
			return nil, "", fmt.Errorf("kubernetes in-cluster config (set kubernetes.kubeconfig to run outside a cluster): %w", err)
		}
		if b, err := os.ReadFile(namespaceFile); err == nil {
			ns = strings.TrimSpace(string(b))
		}
	} else {
		cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: c.Kubeconfig},
			&clientcmd.ConfigOverrides{CurrentContext: c.Context})
		var err error
		if rc, err = cc.ClientConfig(); err != nil {
			return nil, "", fmt.Errorf("kubeconfig %s: %w", c.Kubeconfig, err)
		}
		if ns, _, err = cc.Namespace(); err != nil {
			return nil, "", fmt.Errorf("kubeconfig %s namespace: %w", c.Kubeconfig, err)
		}
	}
	rc.UserAgent = userAgent
	// A handful of lists at start and long-running watches after that.
	rc.QPS, rc.Burst = 5, 10
	cs, err := kubernetes.NewForConfig(rc)
	if err != nil {
		return nil, "", fmt.Errorf("kubernetes client: %w", err)
	}
	return cs, ns, nil
}

// Namespaces returns the configured namespaces, or the one natslens runs
// in when none are configured.
func Namespaces(c config.Kubernetes, own string) ([]string, error) {
	if len(c.Namespaces) > 0 {
		return c.Namespaces, nil
	}
	if own == "" {
		return nil, errors.New("kubernetes.namespaces is empty and the namespace natslens runs in is unknown")
	}
	return []string{own}, nil
}

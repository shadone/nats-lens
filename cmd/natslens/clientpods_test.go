package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/events"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
)

// Without a kubernetes section natslens runs, collects and never makes a
// Kubernetes client, so it asks the API server nothing.
func TestRunWithoutKubernetesMakesNoClient(t *testing.T) {
	var made atomic.Int32
	defer func(old func(config.Kubernetes, string) (kubernetes.Interface, string, error)) { newK8sClient = old }(newK8sClient)
	newK8sClient = func(config.Kubernetes, string) (kubernetes.Interface, string, error) {
		made.Add(1)
		return nil, "", errors.New("no cluster in this test")
	}
	c := testcluster.Start(t, 1)
	dir := t.TempDir()
	conf := filepath.Join(dir, "natslens.yaml")
	body := fmt.Sprintf("interval: 2s\ntimeout: 500ms\nshutdown_grace: 1s\nchecks: { timeout: 1s }\ndata_dir: %s\nlisten: \"\"\nnats: { url: %q, user: %s, password: %s }\n",
		filepath.Join(dir, "data"), c.URL(0), testcluster.SysUser, testcluster.SysPass)
	if err := os.WriteFile(conf, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	st, stop := startRunStore(t, conf)
	waitForStoredCount(t, st, "an epoch", `SELECT count(*) FROM epochs`, 1)
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if n := made.Load(); n != 0 {
		t.Fatalf("a Kubernetes client was made %d times", n)
	}
}

// requests records the requests made of a fake clientset as
// "verb resource namespace selector".
func requests(cs *fake.Clientset) func() []string {
	var mu sync.Mutex
	var out []string
	note := func(a k8stesting.Action) {
		sel := ""
		if l, ok := a.(k8stesting.ListActionImpl); ok {
			sel = l.ListOptions.LabelSelector
		}
		mu.Lock()
		out = append(out, strings.TrimSpace(a.GetVerb()+" "+a.GetResource().Resource+" "+a.GetNamespace()+" "+sel))
		mu.Unlock()
	}
	cs.PrependReactor("*", "*", func(a k8stesting.Action) (bool, runtime.Object, error) { note(a); return false, nil, nil })
	cs.PrependWatchReactor("*", func(a k8stesting.Action) (bool, watch.Interface, error) { note(a); return false, nil, nil })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string{}, out...)
	}
}

// The client pod watch is opt-in: with kubernetes.enabled and no
// client_pod_watch only the NATS pods are asked for; with it, the pods of
// the listed namespace are listed without a selector and written.
func TestKubernetesClientPodWatchOptIn(t *testing.T) {
	for _, on := range []bool{false, true} {
		t.Run(fmt.Sprintf("client_pod_watch %v", on), func(t *testing.T) {
			ctx := context.Background()
			st, err := store.OpenPath(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			cart := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "cart-1", Namespace: "shop", UID: "uid-cart-1"},
				Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIPs: []corev1.PodIP{{IP: "10.4.0.1"}}}}
			// A pod stored by an earlier run in a namespace no longer
			// watched.
			if err := st.WriteClientPods(ctx, store.ClientPodBatch{At: time.Now().Add(-time.Hour),
				Seen: []store.ClientPod{{UID: "uid-old", Namespace: "old", Name: "old-1", IPs: []string{"10.9.0.1"}}}}); err != nil {
				t.Fatal(err)
			}
			cs := fake.NewClientset(cart)
			reqs := requests(cs)
			defer func(old func(config.Kubernetes, string) (kubernetes.Interface, string, error)) { newK8sClient = old }(newK8sClient)
			newK8sClient = func(config.Kubernetes, string) (kubernetes.Interface, string, error) { return cs, "nats", nil }
			cfg := config.Default().Kubernetes
			cfg.Enabled, cfg.Nodes = true, false
			if on {
				cfg.ClientPodWatch = config.ClientPodWatch{Namespaces: []string{"shop"}}
			}
			stop, err := startKubernetes(ctx, cfg, st, events.New(events.Options{Log: quietLog()}), quietLog())
			if err != nil {
				t.Fatal(err)
			}
			waitFor(t, "the NATS pod watch", 5*time.Second, func() bool {
				for _, r := range reqs() {
					if r == "watch pods nats" {
						return true
					}
				}
				return false
			})
			if on {
				waitFor(t, "the client pod written", 20*time.Second, func() bool {
					var n int
					return st.DB().QueryRow(`SELECT count(*) FROM client_pods WHERE pod = 'cart-1' AND ip = '10.4.0.1'`).Scan(&n) == nil && n == 1
				})
			} else {
				time.Sleep(200 * time.Millisecond)
			}
			stop()
			var live int
			if err := st.DB().QueryRow(`SELECT count(*) FROM client_pods WHERE uid = 'uid-old' AND ended_at IS NULL`).Scan(&live); err != nil {
				t.Fatal(err)
			}
			if wantLive := map[bool]int{false: 1, true: 0}[on]; live != wantLive {
				t.Errorf("the earlier run's pod live %d, want %d", live, wantLive)
			}
			shop := false
			for _, r := range reqs() {
				if strings.Contains(r, " shop") {
					shop = true
					if r != "list pods shop" && r != "watch pods shop" {
						t.Errorf("client pod request %q", r)
					}
				} else if strings.HasPrefix(r, "list pods") && r != "list pods nats "+cfg.NATSSelector {
					t.Errorf("pods listed without the NATS selector: %q", r)
				}
			}
			if shop != on {
				t.Fatalf("client pod requests %v with client_pod_watch %v: %v", shop, on, reqs())
			}
		})
	}
}

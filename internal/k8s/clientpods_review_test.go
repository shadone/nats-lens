package k8s

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/shadone/nats-lens/internal/events"
)

// From the review: a namespace of 10,500 live pods, listed in pages of 500 as
// the API server does. The relist must name every live pod, or the store
// ends the ones it leaves out.
func TestReviewClientPodListBeyondTwentyPages(t *testing.T) {
	const n = 10_500
	pods := make([]corev1.Pod, n)
	for i := range pods {
		pods[i] = *clientPod("shop", fmt.Sprintf("p-%05d", i), fmt.Sprintf("10.%d.%d.%d", i/65536, (i/256)%256, i%256), "", "", nil)
	}
	cs := fake.NewClientset()
	cs.PrependReactor("list", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		o := a.(k8stesting.ListActionImpl).ListOptions
		from, _ := strconv.Atoi(o.Continue)
		to := min(n, from+int(o.Limit))
		l := &corev1.PodList{Items: pods[from:to]}
		l.ResourceVersion = "1"
		if to < n {
			l.Continue = strconv.Itoa(to)
		}
		return true, l, nil
	})
	cs.PrependWatchReactor("pods", func(k8stesting.Action) (bool, watch.Interface, error) { return true, watch.NewFake(), nil })
	var got batches
	w := NewClientPodWatcher(ClientPodOptions{Client: cs, Namespaces: []string{"shop"}, Write: got.write, Record: func(events.Event) {},
		FlushEvery: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()
	got.wait(t, "a relist", func(b ClientPodBatch) bool { return len(b.Relists) > 0 })
	got.mu.Lock()
	defer got.mu.Unlock()
	for _, b := range got.all {
		for _, r := range b.Relists {
			if len(r.Listed) != n {
				t.Errorf("relist names %d of %d live pods; the store ends the other %d", len(r.Listed), n, n-len(r.Listed))
			}
		}
	}
}

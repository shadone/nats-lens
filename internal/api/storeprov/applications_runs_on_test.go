package storeprov_test

import (
	"slices"
	"testing"
	"time"
)

// Runs on matches each connection's address to the pod that held it when
// the epoch started: a reused address to the pod of the time, a
// host-network pod's or a doubly held address to none (ambiguous), and an
// address no pod held then to none (unmatched). A failing client pod
// watch is named until its gap is recorded.
func TestStoreApplicationsRunsOn(t *testing.T) {
	c := appsClient(t, appsProvider(t, appsLast))
	res := appsPage(t, c, appsLast.Add(time.Minute), "", false)
	if !res.GetClientPodsWatched() {
		t.Fatal("client pods not watched")
	}
	if u := res.GetClientPodsUnwatched(); len(u) != 1 || u[0].GetSource() != "client pods in payments" ||
		!u[0].GetSince().AsTime().Equal(appsLast.Add(-21*time.Minute)) || !res.GetClientPodsCapped() || u[0].GetError() != "Kubernetes API returned HTTP 403" {
		t.Fatalf("unwatched %v", u)
	}
	rw := appNamed(res, "resize-worker").GetRunsOn()
	if rw.GetMatched() != 5 || rw.GetAmbiguous() != 0 || rw.GetPods() != 4 || !slices.Equal(rw.GetNodes(), []string{"node-1", "node-2"}) ||
		len(rw.GetWorkloads()) != 1 || rw.GetWorkloads()[0].GetNamespace() != "media" || rw.GetWorkloads()[0].GetKind() != "Deployment" ||
		rw.GetWorkloads()[0].GetPods() != 4 || rw.GetWorkloads()[0].GetConnections() != 5 {
		t.Errorf("resize-worker runs on %v", rw)
	}
	cd := appNamed(res, "callback-dispatcher").GetRunsOn()
	if cd.GetMatched() != 1 || cd.GetWorkloads()[0].GetKind() != "StatefulSet" {
		t.Errorf("callback-dispatcher runs on %v", cd)
	}
	// 10.1.0.8 host network and 10.1.0.10 two pods: ambiguous; 10.1.0.9
	// the new pod; 10.1.0.11 ended and 10.1.0.17 not started: unmatched.
	os := appNamed(res, "order-service").GetRunsOn()
	if os.GetMatched() != 1 || os.GetAmbiguous() != 2 || os.GetPods() != 1 || len(os.GetWorkloads()) != 1 || os.GetWorkloads()[0].GetName() != "order-service" ||
		!slices.Equal(os.GetNodes(), []string{"node-2"}) {
		t.Errorf("order-service runs on %v", os)
	}
	if om := appNamed(res, "ops-monitor").GetRunsOn(); om == nil || om.GetMatched() != 0 {
		t.Errorf("ops-monitor runs on %v", om)
	}

	// Three hours before, rw-a's address was rw-a-old's, on node-3; no
	// watch was failing.
	past := appsPage(t, c, appsLast.Add(-3*time.Hour+time.Minute), "", false)
	if len(past.GetClientPodsUnwatched()) != 0 || past.GetClientPodsCapped() {
		t.Errorf("unwatched then: %v, capped %v", past.GetClientPodsUnwatched(), past.GetClientPodsCapped())
	}
	if p := appNamed(past, "resize-worker").GetRunsOn(); p.GetMatched() != 2 || !slices.Equal(p.GetNodes(), []string{"node-2", "node-3"}) {
		t.Errorf("resize-worker then %v", p)
	}
}

// Without kubernetes.client_pod_watch the page reads no client pod and
// says nothing of where applications run, whatever the store holds.
func TestStoreApplicationsRunsOnOff(t *testing.T) {
	p := appsProvider(t, appsLast)
	p.ClientPodWatch = false
	res := appsPage(t, appsClient(t, p), appsLast.Add(time.Minute), "", false)
	if res.GetClientPodsWatched() || len(res.GetClientPodsUnwatched()) != 0 {
		t.Fatalf("watched %v, unwatched %v", res.GetClientPodsWatched(), res.GetClientPodsUnwatched())
	}
	for _, a := range res.GetApplications() {
		if a.GetRunsOn() != nil {
			t.Errorf("%s runs on %v", a.GetName(), a.GetRunsOn())
		}
	}
}

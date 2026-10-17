package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/testcluster"
)

// badKubeconfig does not parse; goodKubeconfig parses and names a
// namespace, for an API server that is not there.
const (
	badKubeconfig  = "not: [a kubeconfig"
	goodKubeconfig = `apiVersion: v1
kind: Config
clusters: [{name: c, cluster: {server: "https://127.0.0.1:1"}}]
users: [{name: u, user: {token: "t"}}]
contexts: [{name: x, context: {cluster: c, user: u, namespace: nats}}]
current-context: x
`
)

// lockedBuffer is a log destination the test reads while it is written.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) count(s string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Count(b.buf.String(), s)
}

// k8sSystemConfig is one system on url with an account connection and
// Kubernetes watching through the kubeconfig file.
func k8sSystemConfig(t *testing.T, dir, url, kubeconfig string) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = filepath.Join(dir, "data")
	cfg.Systems = []config.SystemConfig{{
		ID:         "shop-staging",
		NATS:       config.NATS{URL: url, User: testcluster.SysUser, Password: testcluster.SysPass},
		Accounts:   []config.Account{{Name: "APP", User: testcluster.AppUser, Password: testcluster.AppPass}},
		Kubernetes: config.Default().Kubernetes,
	}}
	cfg.Systems[0].Kubernetes.Enabled = true
	cfg.Systems[0].Kubernetes.Kubeconfig = kubeconfig
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// Every failed attempt stops what it started: after the cancel no
// connection of the system is left on the server and no goroutine of it
// is left running.
func TestFailedSetupAttemptsCleanUp(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a NATS server")
	}
	c := testcluster.Start(t, 1)
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte(badKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := k8sSystemConfig(t, dir, c.URL(0), kubeconfig)
	logs := &lockedBuffer{}
	rts, err := openSystems(context.Background(), cfg, cfg.ResolvedSystems(), slog.New(slog.NewTextHandler(logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer closeSystems(rts, time.Time{})
	rt := rts[0]
	rt.startBackoff = redialBackoff{first: 50 * time.Millisecond, max: 100 * time.Millisecond, reset: time.Minute}
	col := collect.New(nil, collect.Options{Window: 300 * time.Millisecond, ConnzLimit: 10,
		Tracker: collect.NewTracker(time.Hour), Log: quietLog()})
	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() { _, ok := rt.startRetrying(ctx, col); done <- ok }()
	waitFor(t, "three failed attempts", 20*time.Second, func() bool { return logs.count("system not started") >= 3 })
	cancel()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("setup succeeded with a kubeconfig that does not parse")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("startRetrying did not return after the cancel")
	}
	waitFor(t, "no connection left on the server", 10*time.Second, func() bool { return c.Servers[0].NumClients() == 0 })
	waitFor(t, "the goroutines of the attempts to end", 10*time.Second, func() bool { return runtime.NumGoroutine() <= before })
}

// A system whose setup fails at first starts collecting once the problem
// is fixed, without a restart.
func TestSystemStartsOnceItsSetupIsFixed(t *testing.T) {
	if testing.Short() {
		t.Skip("starts NATS servers")
	}
	prod, staging := testcluster.Start(t, 1), testcluster.Start(t, 1)
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte(badKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(dir, "natslens.yaml")
	body := fmt.Sprintf(`interval: 2s
timeout: 500ms
shutdown_grace: 1s
checks: { timeout: 1s }
data_dir: %s
listen: 127.0.0.1:%d
systems:
  - id: shop-prod
    nats: { url: %q, user: %s, password: %s }
  - id: shop-staging
    nats: { url: %q, user: %s, password: %s }
    kubernetes: { enabled: true, kubeconfig: %s }
`, filepath.Join(dir, "data"), testcluster.FreePort(t), prod.URL(0), testcluster.SysUser, testcluster.SysPass,
		staging.URL(0), testcluster.SysUser, testcluster.SysPass, kubeconfig)
	if err := os.WriteFile(conf, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := runWithStatus(t, conf)
	waitFor(t, "shop-staging connection_closed", 20*time.Second, func() bool {
		return statusOf(t, reg, "shop-staging").State == api.CollectionConnectionClosed
	})
	if err := os.WriteFile(kubeconfig, []byte(goodKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "shop-staging collecting with an epoch", 30*time.Second, func() bool {
		s := statusOf(t, reg, "shop-staging")
		return s.State == api.CollectionCollecting && !s.LastEpochAt.IsZero()
	})
}

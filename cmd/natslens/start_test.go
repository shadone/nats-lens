package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
)

// Files a system needs that are missing refuse the start before any
// database opens, with one error that names every broken system.
func TestRunRefusesMissingFilesOfEverySystem(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "natslens.yaml")
	body := fmt.Sprintf(`data_dir: %s
listen: ""
systems:
  - id: shop-prod
    nats: { url: "nats://127.0.0.1:1" }
    inventory_file: %s
  - id: shop-staging
    nats: { url: "nats://127.0.0.1:2", creds: %s }
  - id: shop-edge
    nats: { url: "nats://127.0.0.1:3" }
`, filepath.Join(dir, "data"), filepath.Join(dir, "missing-inventory.yaml"), filepath.Join(dir, "missing.creds"))
	if err := os.WriteFile(conf, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	err := run(context.Background(), []string{"run", "-config", conf, "-log-level", "error"})
	if err == nil {
		t.Fatal("started with missing files")
	}
	for _, want := range []string{"shop-prod", "missing-inventory.yaml", "shop-staging", "nats.creds", "missing.creds"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not name %q:\n%v", want, err)
		}
	}
	if strings.Contains(err.Error(), "shop-edge") {
		t.Errorf("error names the healthy system:\n%v", err)
	}
	for _, id := range []string{"shop-prod", "shop-staging", "shop-edge"} {
		if _, err := os.Stat(filepath.Join(dir, "data", id, store.FileName)); !os.IsNotExist(err) {
			t.Errorf("%s: a database was opened before the check: %v", id, err)
		}
	}
}

// runWithStatus starts natslens run on conf and returns the API's systems.
func runWithStatus(t *testing.T, conf string) api.Systems {
	t.Helper()
	var mu sync.Mutex
	var reg api.Systems
	startRun(t, conf, func(s api.Systems) {
		mu.Lock()
		defer mu.Unlock()
		reg = s
	})
	waitFor(t, "the systems", 20*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return reg != nil
	})
	mu.Lock()
	defer mu.Unlock()
	return reg
}

func statusOf(t *testing.T, reg api.Systems, id string) api.CollectionStatus {
	t.Helper()
	s, err := reg.Resolve(id)
	if err != nil {
		t.Fatal(err)
	}
	return s.Status()
}

// waitForEpochs waits until the system stored n epochs after the call.
func waitForEpochs(t *testing.T, reg api.Systems, id string, n int) {
	t.Helper()
	last := statusOf(t, reg, id).LastEpochAt
	for range n {
		waitFor(t, id+" storing epochs", 20*time.Second, func() bool {
			at := statusOf(t, reg, id).LastEpochAt
			if at.After(last) {
				last = at
				return true
			}
			return false
		})
	}
}

// A system whose credentials are rejected at start leaves the other
// system collecting, and natslens running.
func TestRunIsolatesASystemWithRejectedCredentials(t *testing.T) {
	if testing.Short() {
		t.Skip("starts NATS servers")
	}
	prod, staging := testcluster.Start(t, 1), testcluster.Start(t, 1)
	staging.SetSysPassword(testcluster.SysPass, "rotated-elsewhere")
	dir := t.TempDir()
	conf := twoSystemsConfig(t, dir, testcluster.FreePort(t), prod.URL(0), staging.URL(0))
	reg := runWithStatus(t, conf)
	waitForEpochs(t, reg, "shop-prod", 2)
	if got := statusOf(t, reg, "shop-staging"); got.State == api.CollectionCollecting || got.LastError == "" ||
		!got.LastEpochAt.IsZero() {
		t.Errorf("shop-staging status %+v", got)
	}
}

// A system whose setup fails after the start check (here a kubeconfig
// that does not parse) is isolated: its status is connection_closed with
// the error, and the other system keeps collecting.
func TestRunIsolatesASystemWhoseSetupFails(t *testing.T) {
	if testing.Short() {
		t.Skip("starts NATS servers")
	}
	prod, staging := testcluster.Start(t, 1), testcluster.Start(t, 1)
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("not: [a kubeconfig"), 0o600); err != nil {
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
	if got := statusOf(t, reg, "shop-staging"); !strings.Contains(got.LastError, "kubeconfig") {
		t.Errorf("shop-staging last error %q", got.LastError)
	}
	waitForEpochs(t, reg, "shop-prod", 2)
	if got := statusOf(t, reg, "shop-staging"); got.State != api.CollectionConnectionClosed || !got.LastEpochAt.IsZero() {
		t.Errorf("shop-staging status %+v", got)
	}
}

func TestMissingFiles(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "present")
	if err := os.WriteFile(present, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing")
	sys := config.System{ID: "shop-prod",
		NATS:       config.NATS{Creds: present, TLS: config.TLS{CA: missing}},
		Accounts:   []config.Account{{Name: "APP", Creds: missing}},
		Kubernetes: config.Kubernetes{Enabled: true, Kubeconfig: missing},
	}
	err := missingFiles(sys, true)
	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range []string{"nats.tls.ca", "kubernetes.kubeconfig"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not name %s: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "nats.creds") {
		t.Errorf("present file named: %v", err)
	}
	if strings.Contains(err.Error(), "accounts[") {
		t.Errorf("an account file named: %v", err)
	}
	err = missingFiles(sys, false)
	if err == nil || !strings.Contains(err.Error(), "nats.tls.ca") ||
		strings.Contains(err.Error(), "accounts[") || strings.Contains(err.Error(), "kubeconfig") {
		t.Errorf("without events: %v, want only nats.tls.ca", err)
	}
}

// writeAccountConfig writes a one-system configuration whose account
// names a creds file that does not exist.
func writeAccountConfig(t *testing.T, dir, url, events string) string {
	t.Helper()
	conf := filepath.Join(dir, "natslens.yaml")
	body := fmt.Sprintf(`data_dir: %s
listen: ""
events: { enabled: %s }
nats: { url: %q, user: %s, password: %s }
accounts: [{ name: APP, creds: %s }]
`, filepath.Join(dir, "data"), events, url, testcluster.SysUser, testcluster.SysPass, filepath.Join(dir, "missing-app.creds"))
	if err := os.WriteFile(conf, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return conf
}

// An account file that is missing refuses no start: once starts with
// one missing, with events on or off. run is covered by
// TestRunWithEventsStartsWithoutABadAccount.
func TestStartCheckOnlyChecksFilesTheCommandReads(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a NATS server")
	}
	c := testcluster.Start(t, 1)
	ctx := context.Background()
	for _, events := range []string{"true", "false"} {
		conf := writeAccountConfig(t, t.TempDir(), c.URL(0), events)
		if err := run(ctx, []string{"once", "-config", conf, "-log-level", "error"}); err != nil {
			t.Errorf("once with events %s: %v", events, err)
		}
	}
}

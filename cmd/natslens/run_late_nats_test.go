package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/testcluster"
)

// natslens started before NATS keeps running, serves the API and is ready
// (so the UI stays reachable), shows no epoch, and starts taking epochs
// once the server comes up.
func TestRunWaitsForNATS(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a NATS server")
	}
	natsPort, apiPort := testcluster.FreePort(t), testcluster.FreePort(t)
	dir := t.TempDir()
	conf := filepath.Join(dir, "natslens.yaml")
	body := fmt.Sprintf(`interval: 2s
timeout: 500ms
shutdown_grace: 1s
checks: { timeout: 1s }
data_dir: %s
listen: 127.0.0.1:%d
nats: { url: "nats://127.0.0.1:%d", user: %s, password: %s }
`, filepath.Join(dir, "data"), apiPort, natsPort, testcluster.SysUser, testcluster.SysPass)
	if err := os.WriteFile(conf, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"run", "-config", conf, "-log-level", "error"}) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Error("run did not stop")
		}
	}()
	ready := func() int {
		res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/readyz", apiPort))
		if err != nil {
			return 0
		}
		res.Body.Close()
		return res.StatusCode
	}
	time.Sleep(3 * time.Second)
	select {
	case err := <-done:
		done <- err
		t.Fatalf("run ended without NATS: %v", err)
	default:
	}
	if got := ready(); got != http.StatusOK {
		t.Fatalf("readyz without NATS = %d, want 200: readiness must not follow collection", got)
	}
	if epochAge(t, apiPort) {
		t.Fatal("an epoch age is reported although NATS was never reachable")
	}
	testcluster.StartOnPort(t, natsPort)
	deadline := time.Now().Add(20 * time.Second)
	for !epochAge(t, apiPort) {
		if time.Now().After(deadline) {
			t.Fatal("no epoch 20 s after NATS started")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// epochAge reports whether /metrics carries nats_lens_last_epoch_age_seconds,
// which is absent until the first epoch is stored.
func epochAge(t *testing.T, apiPort int) bool {
	t.Helper()
	res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/metrics", apiPort))
	if err != nil {
		return false
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return false
	}
	return strings.Contains(string(b), "\nnats_lens_last_epoch_age_seconds{")
}

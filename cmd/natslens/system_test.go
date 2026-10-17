package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/config"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
)

// twoSystemsConfig writes a configuration listing shop-prod and
// shop-staging with the NATS URLs given, and returns its path.
func twoSystemsConfig(t *testing.T, dir string, apiPort int, prodURL, stagingURL string) string {
	t.Helper()
	conf := filepath.Join(dir, "natslens.yaml")
	body := fmt.Sprintf(`interval: 2s
timeout: 500ms
shutdown_grace: 1s
checks: { timeout: 1s }
data_dir: %s
listen: 127.0.0.1:%d
systems:
  - id: shop-prod
    name: Production
    nats: { url: %q, user: %s, password: %s }
  - id: shop-staging
    nats: { url: %q, user: %s, password: %s }
`, filepath.Join(dir, "data"), apiPort, prodURL, testcluster.SysUser, testcluster.SysPass,
		stagingURL, testcluster.SysUser, testcluster.SysPass)
	if err := os.WriteFile(conf, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return conf
}

// startRun runs natslens run on the configuration until the test ends,
// with observe told the API's systems.
func startRun(t *testing.T, conf string, observe func(api.Systems)) (stop func() error) {
	t.Helper()
	cfg, err := config.Parse("natslens run", []string{"-config", conf, "-log-level", "error"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runConfig(ctx, "run", cfg, "", quietLog(), observe) }()
	var once sync.Once
	var err2 error
	stop = func() error {
		once.Do(func() {
			cancel()
			select {
			case err2 = <-done:
			case <-time.After(30 * time.Second):
				err2 = fmt.Errorf("run did not stop")
			}
		})
		return err2
	}
	t.Cleanup(func() { _ = stop() })
	return stop
}

// epochAgeOf reports whether /metrics has an epoch age for the system.
func epochAgeOf(t *testing.T, apiPort int, system string) bool {
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
	return strings.Contains(string(b), fmt.Sprintf("\nnats_lens_last_epoch_age_seconds{system=%q", system))
}

// storedServers lists a database's server names and IDs.
func storedServers(t *testing.T, dataDir string) (names, ids []string) {
	t.Helper()
	st, err := store.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.DB().Query(`SELECT server_name, server_id FROM servers ORDER BY server_name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n, id string
		if err := rows.Scan(&n, &id); err != nil {
			t.Fatal(err)
		}
		names, ids = append(names, n), append(ids, id)
	}
	return names, ids
}

func serverIDs(c *testcluster.Cluster) []string {
	var out []string
	for _, s := range c.Servers {
		out = append(out, s.ID())
	}
	sort.Strings(out)
	return out
}

// Two systems in one process: each database holds only its own servers,
// and the API answers each system from its own database.
func TestRunTwoSystems(t *testing.T) {
	if testing.Short() {
		t.Skip("starts NATS servers")
	}
	prod, staging := testcluster.Start(t, 1), testcluster.Start(t, 2)
	dir, apiPort := t.TempDir(), testcluster.FreePort(t)
	var mu sync.Mutex
	var reg api.Systems
	stop := startRun(t, twoSystemsConfig(t, dir, apiPort, prod.URL(0), staging.URL(0)), func(s api.Systems) {
		mu.Lock()
		defer mu.Unlock()
		reg = s
	})
	waitFor(t, "an epoch of each system", 20*time.Second, func() bool {
		return epochAgeOf(t, apiPort, "shop-prod") && epochAgeOf(t, apiPort, "shop-staging")
	})
	mu.Lock()
	systems := reg
	mu.Unlock()
	waitForEpochs(t, systems, "shop-prod", 1)
	waitForEpochs(t, systems, "shop-staging", 1)

	client := natslensv1connect.NewLensServiceClient(http.DefaultClient, fmt.Sprintf("http://127.0.0.1:%d", apiPort)+api.PathPrefix)
	for _, c := range []struct {
		system string
		want   []string
	}{{"shop-prod", []string{"nats-0"}}, {"shop-staging", []string{"nats-0", "nats-1"}}, {"", []string{"nats-0"}}} {
		res, err := client.ListServers(context.Background(), connect.NewRequest(&v1.ListServersRequest{System: c.system}))
		if err != nil {
			t.Fatalf("ListServers{system:%q}: %v", c.system, err)
		}
		var got []string
		for _, s := range res.Msg.GetServers() {
			got = append(got, s.GetName())
		}
		sort.Strings(got)
		if !slices.Equal(got, c.want) {
			t.Errorf("ListServers{system:%q} = %v, want %v", c.system, got, c.want)
		}
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		id    string
		names []string
		ids   []string
	}{{"shop-prod", []string{"nats-0"}, serverIDs(prod)}, {"shop-staging", []string{"nats-0", "nats-1"}, serverIDs(staging)}} {
		dataDir := filepath.Join(dir, "data", c.id)
		names, ids := storedServers(t, dataDir)
		sort.Strings(ids)
		if !slices.Equal(names, c.names) || !slices.Equal(ids, c.ids) {
			t.Errorf("%s stored servers %v %v, want %v %v", c.id, names, ids, c.names, c.ids)
		}
		st, err := store.Open(context.Background(), dataDir)
		if err != nil {
			t.Fatal(err)
		}
		var epochs int
		if err := st.DB().QueryRow(`SELECT count(*) FROM epochs`).Scan(&epochs); err != nil {
			t.Fatal(err)
		}
		st.Close()
		if epochs < 2 {
			t.Errorf("%s stored %d epochs, want at least 2", c.id, epochs)
		}
		if _, err := os.Stat(filepath.Join(dataDir, "archive", "state.json")); err != nil {
			t.Errorf("%s: no state.json at shutdown: %v", c.id, err)
		}
	}
}

// A system whose NATS cannot be reached stops neither the other system's
// epochs nor readiness, and its status says not_connected.
func TestRunWithOneSystemUnreachable(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a NATS server")
	}
	prod := testcluster.Start(t, 1)
	dir, apiPort := t.TempDir(), testcluster.FreePort(t)
	down := fmt.Sprintf("nats://127.0.0.1:%d", testcluster.FreePort(t))
	var mu sync.Mutex
	var systems api.Systems
	startRun(t, twoSystemsConfig(t, dir, apiPort, prod.URL(0), down), func(s api.Systems) {
		mu.Lock()
		defer mu.Unlock()
		systems = s
	})
	waitFor(t, "an epoch of shop-prod", 20*time.Second, func() bool { return epochAgeOf(t, apiPort, "shop-prod") })
	res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/readyz", apiPort))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("readyz = %d with one system unreachable", res.StatusCode)
	}
	mu.Lock()
	reg := systems
	mu.Unlock()
	if len(reg) != 2 {
		t.Fatalf("registry has %d systems", len(reg))
	}
	waitFor(t, "shop-staging not_connected", 10*time.Second, func() bool {
		s, err := reg.Resolve("shop-staging")
		return err == nil && s.Status != nil && s.Status().State == api.CollectionNotConnected
	})
	p, err := reg.Resolve("shop-prod")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Status(); got.State != api.CollectionCollecting || got.LastEpochAt.IsZero() {
		t.Errorf("shop-prod status %+v", got)
	}
	if p.Ref.GetName() != "Production" {
		t.Errorf("shop-prod name %q", p.Ref.GetName())
	}
	if epochAgeOf(t, apiPort, "shop-staging") {
		t.Error("shop-staging reports an epoch although it was never reachable")
	}
}

func epochCount(t *testing.T, dataDir string) int {
	t.Helper()
	st, err := store.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var n int
	if err := st.DB().QueryRow(`SELECT count(*) FROM epochs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// once stores one epoch of every system, or of the one -system names.
func TestOnceEverySystemOrOne(t *testing.T) {
	if testing.Short() {
		t.Skip("starts NATS servers")
	}
	prod, staging := testcluster.Start(t, 1), testcluster.Start(t, 1)
	dir := t.TempDir()
	conf := twoSystemsConfig(t, dir, 0, prod.URL(0), staging.URL(0))
	ctx := context.Background()
	if err := run(ctx, []string{"once", "-config", conf, "-log-level", "error"}); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, []string{"once", "-config", conf, "-log-level", "error", "-system", "shop-staging"}); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]int{"shop-prod": 1, "shop-staging": 2} {
		if got := epochCount(t, filepath.Join(dir, "data", id)); got != want {
			t.Errorf("%s: %d epochs, want %d", id, got, want)
		}
	}
}

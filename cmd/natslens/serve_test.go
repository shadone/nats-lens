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

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/fake"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/testcluster"
)

// hungMetrics is a provider whose metrics never come.
type hungMetrics struct{ api.Provider }

func (hungMetrics) WriteMetrics(ctx context.Context, _ *api.Exposition) error {
	<-ctx.Done()
	return ctx.Err()
}

func metricsTimeoutConfig(t *testing.T) config.Config {
	t.Helper()
	conf := filepath.Join(t.TempDir(), "natslens.yaml")
	if err := os.WriteFile(conf, []byte("metrics_timeout: 300ms\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Parse("natslens run", []string{"-config", conf})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestMetricsTimeoutOptions(t *testing.T) {
	o := apiOptionsFor(metricsTimeoutConfig(t), nil)
	if o.metricsTimeout != 300*time.Millisecond {
		t.Fatalf("metricsTimeout = %v, want 300ms", o.metricsTimeout)
	}
}

// metrics_timeout from the configuration reaches the API: a system whose
// metrics hang fails the scrape after it, not after the 8 s default.
func TestMetricsTimeoutFromConfig(t *testing.T) {
	cfg := metricsTimeoutConfig(t)
	port := testcluster.FreePort(t)
	cfg.Listen = fmt.Sprintf("127.0.0.1:%d", port)
	o := apiOptionsFor(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	wait, err := startAPI(ctx, o, api.One("shop-prod", "shop-prod", hungMetrics{fake.New(false)}), quietLog())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); wait() }()
	start := time.Now()
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/metrics", port))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("the scrape took %v with metrics_timeout 300ms", took)
	}
	if res.StatusCode != http.StatusInternalServerError || !strings.Contains(string(body), "metrics unavailable") {
		t.Fatalf("/metrics: %d %s", res.StatusCode, body)
	}
}

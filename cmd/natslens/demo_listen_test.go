package main

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/testcluster"
)

func TestDemoListenDefault(t *testing.T) {
	t.Setenv(demoListenEnv, "")
	if got := demoListen(); got != api.DefaultListen {
		t.Fatalf("demoListen() = %q, want %q", got, api.DefaultListen)
	}
}

// NATSLENS_DEMO_LISTEN sets the address natslens demo serves on when
// -listen is not given, as the container image does.
func TestDemoListensOnEnvAddress(t *testing.T) {
	port := testcluster.FreePort(t)
	t.Setenv(demoListenEnv, fmt.Sprintf("127.0.0.1:%d", port))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runDemo(ctx, nil) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for {
		res, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
		if err == nil {
			res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("/healthz: %d", res.StatusCode)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("natslens demo does not serve on $%s: %v", demoListenEnv, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
	"github.com/shadone/nats-lens/internal/testfatal"
)

func TestFatalSystemIsolationAndReadiness(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data", "shop-prod", store.FileName)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	testfatal.Prepare(t, path)
	c := testcluster.Start(t, 1)
	port := testcluster.FreePort(t)
	regCh := make(chan api.Systems, 1)
	stop := startRun(t, twoSystemsConfig(t, dir, port, c.URL(0), c.URL(0)), func(s api.Systems) { regCh <- s })
	var reg api.Systems
	select {
	case reg = <-regCh:
	case <-time.After(15 * time.Second):
		t.Fatal("runtime did not start")
	}
	probe := func() int {
		res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/readyz", port))
		if err != nil {
			return 0
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	waitFor(t, "ready before fatal", 10*time.Second, func() bool { return probe() == 200 })
	failed := reg[0].Provider.(*storeprov.Provider).St
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := failed.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM fatal_fixture"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); !store.IsFatal(err) {
		t.Fatalf("expected typed fatal; got %T", err)
	}
	waitFor(t, "unready after fatal", 5*time.Second, func() bool { return probe() == 503 })
	healthy := reg[1].Provider.(*storeprov.Provider).St
	var before int
	if err := healthy.DB().QueryRow("SELECT count(*) FROM epochs").Scan(&before); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "healthy system continues storing", 12*time.Second, func() bool {
		var n int
		return healthy.DB().QueryRow("SELECT count(*) FROM epochs").Scan(&n) == nil && n > before+1
	})
	client := natslensv1connect.NewLensServiceClient(http.DefaultClient, fmt.Sprintf("http://127.0.0.1:%d", port)+api.PathPrefix)
	res, err := client.ListSystems(ctx, connect.NewRequest(&v1.ListSystemsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	bad, good := res.Msg.GetSystems()[0], res.Msg.GetSystems()[1]
	if bad.GetError() == "" || strings.Contains(bad.GetError(), "Chunk") || len(bad.GetError()) > 512 {
		t.Error("failed system missing bounded error")
	}
	if bad.GetCollection() != v1.CollectionState_COLLECTION_STATE_CONNECTION_CLOSED {
		t.Errorf("failed collection=%v", bad.GetCollection())
	}
	if good.GetError() != "" {
		t.Errorf("healthy summary error: %s", good.GetError())
	}
	if err := stop(); !store.IsFatal(err) {
		t.Fatalf("shutdown lost fatal: %T", err)
	}
}

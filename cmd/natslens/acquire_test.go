package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/shadone/nats-lens/internal/acquisition"
	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/dataset"
	"github.com/shadone/nats-lens/internal/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAcquireArgumentsAndHelp(t *testing.T) {
	var out bytes.Buffer
	for _, args := range [][]string{nil, {"-socket", "/tmp/unused"}, {"-socket", "/tmp/unused", "-system", "shop-prod", "unexpected"}} {
		if err := runAcquire(context.Background(), args, &out); err == nil {
			t.Fatal("invalid arguments accepted")
		}
	}
	_ = runAcquire(context.Background(), []string{"-help"}, &out)
	for _, want := range []string{"all retained", "cooperative", "quota", "MVCC/WAL"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help omits %s", want)
		}
	}
}

func TestAcquireUsesOnlyExplicitLocalAdminChannel(t *testing.T) {
	ctx := context.Background()
	stop, err := startAcquisition(ctx, acquisitionOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	stop()
	root, err := os.MkdirTemp("/private/tmp", "nlc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Date(2026, 10, 17, 14, 30, 0, 0, time.UTC)
	snap := collect.NewSnapshot(at)
	snap.FinishedAt = at.Add(time.Second)
	if _, err = st.WriteSnapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	rt := &systemRuntime{st: st, cfg: config.Config{Interval: config.Duration(30 * time.Second)}, sys: config.System{ID: "shop-prod", Name: "Shop production", Retention: config.Duration(24 * time.Hour)}}
	socket := filepath.Join(root, "a.sock")
	stop, err = startAcquisition(ctx, acquisitionOptions{socket: socket, directory: root, maxBytes: 64 << 20}, []*systemRuntime{rt})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	var out bytes.Buffer
	if err = runAcquire(ctx, []string{"-socket", socket, "-system", "shop-prod"}, &out); err != nil {
		t.Fatal(err)
	}
	var result acquisition.Result
	if err = json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(result.Path) != root || result.Manifest.Scope != dataset.FullSystemScope || result.Manifest.System.RetentionNS == nil || *result.Manifest.System.RetentionNS != int64(24*time.Hour) {
		t.Fatalf("result lacks captured server-local identity %+v", result)
	}
	d, err := dataset.Open(ctx, result.Path)
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
}

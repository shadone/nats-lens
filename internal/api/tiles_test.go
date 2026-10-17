package api

import (
	"testing"
	"time"
)

func TestTileTextFormats(t *testing.T) {
	for got, want := range map[string]string{
		FmtBytes(512):                   "512 B",
		FmtBytes(17 << 20):              "17 MiB",
		FmtBytes(1.25 * (1 << 30)):      "1.2 GiB",
		FmtGrowth(17 << 20):             "+17 MiB/day",
		FmtGrowth(-2 << 10):             "-2.0 KiB/day",
		FmtUptime(42 * time.Minute):     "up 42 min",
		FmtUptime(47 * time.Hour):       "up 47 h",
		FmtUptime(100 * 24 * time.Hour): "up 100 d",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

// Every tile has a label and a one-sentence description.
func TestEveryTileIsDescribed(t *testing.T) {
	for _, id := range []string{TileServers, TileMetaLeader, TileStreams, TileKV, TileObjectStores, TileConsumers,
		TileAccounts, TileClients, TileStorage} {
		if TileLabels[id] == "" || TileDescriptions[id] == "" {
			t.Errorf("tile %s: label %q, description %q", id, TileLabels[id], TileDescriptions[id])
		}
	}
}

// Storage that no server reported has no line, as the other tiles'.
func TestStorageSubtitleUnset(t *testing.T) {
	if got := StorageSubtitle(nil, nil, nil); got != "" {
		t.Fatalf("subtitle = %q, want empty", got)
	}
	used := float64(1 << 30)
	if got := StorageSubtitle(&used, nil, nil); got != "all replicas" {
		t.Fatalf("subtitle = %q, want \"all replicas\"", got)
	}
}

func TestCountSuffix(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	for _, c := range []struct {
		v    *float64
		word string
		want string
	}{
		{f(1), "bucket", "bucket"},
		{f(2), "bucket", "buckets"},
		{f(0), "bucket", "buckets"},
		{f(1), "durable", "durable"},
		{f(12), "durable", "durables"},
		{f(1), "conn", "conn"},
		{f(182), "conn", "conns"},
		{nil, "bucket", "buckets"},
	} {
		if got := CountSuffix(c.v, c.word); got != c.want {
			t.Errorf("CountSuffix(%v, %q) = %q, want %q", c.v, c.word, got, c.want)
		}
	}
}

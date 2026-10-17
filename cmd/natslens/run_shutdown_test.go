package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/shadone/nats-lens/internal/archive"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
)

// Events still buffered when the process is told to stop are written
// within shutdown_grace plus one flush interval, and the last state.json
// covers them.
func TestShutdownWritesEventsThenState(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a NATS server")
	}
	c := testcluster.Start(t, 1)
	dir := t.TempDir()
	conf := filepath.Join(dir, "natslens.yaml")
	body := fmt.Sprintf(`interval: 10s
timeout: 1s
shutdown_grace: 2s
data_dir: %s
archive_dir: %s
events: { flush_interval: 1m }
nats: { url: %s, user: %s, password: %s }
`, filepath.Join(dir, "data"), filepath.Join(dir, "archive"), c.URL(0), testcluster.SysUser, testcluster.SysPass)
	if err := os.WriteFile(conf, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"run", "-config", conf, "-log-level", "error"}) }()
	time.Sleep(2 * time.Second) // started, first epoch and archive tick done
	// Connections made now produce events that wait in the buffer: the
	// flush interval is a minute.
	for i := range 5 {
		nc, err := nats.Connect(c.URL(0), nats.UserInfo(testcluster.AppUser, testcluster.AppPass), nats.Name(fmt.Sprint("late-", i)))
		if err != nil {
			t.Fatal(err)
		}
		nc.Close()
	}
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 2*time.Second+time.Minute {
		t.Errorf("shutdown took %s", took)
	}
	st, err := store.Open(context.Background(), filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var late, maxID int64
	if err := st.DB().QueryRow(`SELECT count(*) FILTER (WHERE client_name LIKE 'late-%'), max(id) FROM events`).Scan(&late, &maxID); err != nil {
		t.Fatal(err)
	}
	if late == 0 {
		t.Fatal("buffered events not written at shutdown")
	}
	b, err := os.ReadFile(filepath.Join(dir, "archive", archive.StateRelPath))
	if err != nil {
		t.Fatal(err)
	}
	var state archive.State
	if err := json.Unmarshal(b, &state); err != nil {
		t.Fatal(err)
	}
	if state.MaxEventID != maxID {
		t.Fatalf("state.json max_event_id = %d, stored max id = %d: the state was written before the last events", state.MaxEventID, maxID)
	}
}

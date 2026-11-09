package acquisition

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/dataset"
	"github.com/shadone/nats-lens/internal/store"
)

func setup(t *testing.T) (Options, []Source) {
	t.Helper()
	root := shortSocketDir(t, "nla-")
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	snap := collect.NewSnapshot(time.Date(2026, 10, 17, 14, 30, 0, 0, time.UTC))
	snap.FinishedAt = snap.StartedAt.Add(time.Second)
	if _, err = st.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	return Options{Socket: filepath.Join(root, "admin.sock"), Directory: root, ApplicationVersion: "test", Budget: dataset.Options{MaxBytes: 64 << 20}}, []Source{{Store: st, System: dataset.System{ID: "shop-prod", Name: "Shop production", CollectionIntervalNS: int64(30 * time.Second)}}}
}
func TestAcquisitionSocketAndRestart(t *testing.T) {
	o, src := setup(t)
	stop, err := Start(context.Background(), o, src)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	i, err := os.Stat(o.Socket)
	if err != nil || i.Mode().Perm() != 0600 {
		t.Fatalf("socket permissions: %v %v", i, err)
	}
	result, err := Request(context.Background(), o.Socket, "shop-prod")
	if err != nil {
		t.Fatal(err)
	}
	d, err := dataset.Open(context.Background(), result.Path)
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	stop()
	// A completed artifact remains openable after the admin channel restarts.
	stop2, err := Start(context.Background(), o, src)
	if err != nil {
		t.Fatal(err)
	}
	defer stop2()
	d, err = dataset.Open(context.Background(), result.Path)
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	if _, err = Request(context.Background(), o.Socket, "missing"); err == nil {
		t.Fatal("unknown system accepted")
	}
}
func TestAcquisitionRejectsPathInputAndUnsafeRoot(t *testing.T) {
	o, src := setup(t)
	stop, err := Start(context.Background(), o, src)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", o.Socket)
	}}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr}
	for _, body := range []string{`{"system":"shop-prod","path":"/tmp/unrelated"}`, `{"system":"shop-prod"} {}`, strings.Repeat("x", 2048)} {
		resp, err := client.Post("http://unix/acquire", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatalf("path input status %d", resp.StatusCode)
		}
	}
	stop()
	if err = os.WriteFile(o.Socket, []byte("unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	if stop2, err := Start(context.Background(), o, src); err == nil {
		stop2()
		t.Fatal("existing unrelated path overwritten")
	}
	if b, _ := os.ReadFile(o.Socket); string(b) != "unrelated" {
		t.Fatal("unrelated file removed")
	}
	if err = os.Chmod(o.Directory, 0755); err != nil {
		t.Fatal(err)
	}
	if stop2, err := Start(context.Background(), o, src); err == nil {
		stop2()
		t.Fatal("public root accepted")
	}
}
func TestAcquisitionCleanupDoesNotUnlinkReplacement(t *testing.T) {
	o, src := setup(t)
	stop, err := Start(context.Background(), o, src)
	if err != nil {
		t.Fatal(err)
	}
	// Replace our socket path with an unrelated file while retaining its listener.
	if err = os.Remove(o.Socket); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(o.Socket, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	stop()
	if b, _ := os.ReadFile(o.Socket); string(b) != "keep" {
		t.Fatal("replacement removed")
	}
}

func TestAcquisitionConfigurationAndClientFailures(t *testing.T) {
	o, src := setup(t)
	for _, change := range []func(*Options, *[]Source){
		func(o *Options, _ *[]Source) { o.Socket = "" },
		func(o *Options, _ *[]Source) { o.Directory = "" },
		func(o *Options, _ *[]Source) { o.Budget.MaxBytes = -1 },
		func(o *Options, _ *[]Source) { o.Budget.MaxBytes = dataset.MaxDataBytes + 1 },
		func(_ *Options, s *[]Source) { *s = []Source{{}} },
		func(_ *Options, s *[]Source) { *s = append(*s, (*s)[0]) },
	} {
		copyO := o
		copyS := append([]Source{}, src...)
		change(&copyO, &copyS)
		if stop, err := Start(context.Background(), copyO, copyS); err == nil {
			stop()
			t.Fatal("invalid configuration accepted")
		}
	}
	if _, err := Request(context.Background(), o.Socket, ""); err == nil {
		t.Fatal("empty system accepted")
	}
	if _, err := Request(context.Background(), o.Socket, "shop-prod"); err == nil {
		t.Fatal("missing socket accepted")
	}
	if err := os.WriteFile(o.Socket, []byte("ordinary file"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Request(context.Background(), o.Socket, "shop-prod"); err == nil {
		t.Fatal("regular file accepted")
	}
	if err := os.Remove(o.Socket); err != nil {
		t.Fatal(err)
	}
	stop, err := Start(context.Background(), o, src)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Request(ctx, o.Socket, "shop-prod"); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("cancel failure %v", err)
	}
}
func TestAcquisitionProtocolAndSanitizedFailures(t *testing.T) {
	o, src := setup(t)
	o.Budget.MaxBytes = 1
	stop, err := Start(context.Background(), o, src)
	if err != nil {
		t.Fatal(err)
	}
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", o.Socket)
	}}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr}
	for _, test := range []struct {
		method, path, content string
		status                int
	}{{"GET", "/acquire", "application/json", 404}, {"POST", "/unknown", "application/json", 404}, {"POST", "/acquire", "text/plain", 415}} {
		req, err := http.NewRequest(test.method, "http://unix"+test.path, strings.NewReader(`{"system":"shop-prod"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", test.content)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != test.status {
			t.Fatalf("status %d", resp.StatusCode)
		}
	}
	if _, err = Request(context.Background(), o.Socket, "shop-prod"); err == nil || !strings.Contains(err.Error(), "budget_exceeded") {
		t.Fatalf("budget failure %v", err)
	}
	stop()
	o.Budget.MaxBytes = 64 << 20
	if err = src[0].Store.Close(); err != nil {
		t.Fatal(err)
	}
	stop, err = Start(context.Background(), o, src)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if _, err = Request(context.Background(), o.Socket, "shop-prod"); err == nil || !strings.Contains(err.Error(), "acquisition_failed") || strings.Contains(err.Error(), o.Directory) {
		t.Fatalf("unsanitized failure %v", err)
	}
}
func TestAcquisitionStaleSocketIsNotRemoved(t *testing.T) {
	o, src := setup(t)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: o.Socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	ln.Close()
	if stop, err := Start(context.Background(), o, src); err == nil {
		stop()
		t.Fatal("stale socket reused")
	}
	if info, err := os.Lstat(o.Socket); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatal("stale socket removed")
	}
}

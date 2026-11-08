package offline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/dataset"
	"github.com/shadone/nats-lens/internal/investigation"
	"github.com/shadone/nats-lens/internal/store"
)

type denyNetwork struct{ calls *int }

func (d denyNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	*d.calls++
	return nil, errors.New("network disabled")
}

// This runs in the filesystem/network sandbox without listener permission. The
// replaced default transport would also panic in NewRemote's clone, detecting
// accidental construction of remote/profile authentication on the offline path.
func TestOfflineInvestigationNetworkDisabled(t *testing.T) {
	calls := 0
	previous := http.DefaultTransport
	http.DefaultTransport = denyNetwork{&calls}
	defer func() { http.DefaultTransport = previous }()
	path := closedFixture(t)
	before, _ := os.ReadFile(path)
	var out, diagnostic bytes.Buffer
	args := []string{"query", "GetOverview", "-db", path, "-system", "shop", "-collection-interval", "30s", "-url", "https://lens.example.com/?system=shop&range=1h"}
	if err := investigation.RunWithOffline(context.Background(), args, strings.NewReader(""), &out, &diagnostic, Open); err != nil {
		t.Fatalf("%v %s", err, out.String())
	}
	var result struct {
		Response json.RawMessage       `json:"response"`
		Context  investigation.Context `json:"context"`
		Links    []any                 `json:"links"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || len(result.Links) != 0 || !strings.Contains(string(result.Response), `"9007199254740993"`) {
		t.Fatalf("calls %d: %s", calls, out.String())
	}
	if result.Context.Range.End != "2026-10-17T14:29:01Z" {
		t.Fatalf("wall clock leaked: %+v", result.Context.Range)
	}
	input, _ := json.Marshal(result.Context)
	out.Reset()
	args = []string{"query", "GetOverview", "-db", path, "-system", "shop", "-collection-interval", "30s", "-context", "-"}
	if err := investigation.RunWithOffline(context.Background(), args, bytes.NewReader(input), &out, &diagnostic, Open); err != nil {
		t.Fatalf("context replay: %v %s", err, out.String())
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("source mutated")
	}
}

func TestOfflineSourceErrorsAndEngineRestrictions(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.duckdb")
	st, err := store.OpenPath(ctx, empty)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	_, err = Open(ctx, investigation.OfflineSelection{DB: empty, System: "shop", Interval: time.Second})
	var diagnostic *investigation.Diagnostic
	if !errors.As(err, &diagnostic) || diagnostic.Code != "no_reference_clock" {
		t.Fatalf("no reference error: %v", err)
	}
	if _, err = Open(ctx, investigation.OfflineSelection{DB: empty, Bundle: dir}); err == nil {
		t.Fatal("ambiguous direct selection accepted")
	}
	if _, err = Open(ctx, investigation.OfflineSelection{DB: empty}); err == nil {
		t.Fatal("invented identity/cadence")
	}
	path := closedFixture(t)
	db, err := dataset.OpenDatabase(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	var external, autoload, autoinstall bool
	if err = db.QueryRow(`SELECT current_setting('enable_external_access'),current_setting('autoload_known_extensions'),current_setting('autoinstall_known_extensions')`).Scan(&external, &autoload, &autoinstall); err != nil {
		t.Fatal(err)
	}
	if external || autoload || autoinstall {
		t.Fatal("external effects enabled")
	}
	for _, q := range []string{`CREATE TABLE forbidden(i INT)`, `DELETE FROM epochs`, `SELECT * FROM read_csv('/etc/passwd')`} {
		if _, err = db.ExecContext(ctx, q); err == nil {
			t.Fatalf("external effect/write permitted: %s", q)
		}
	}
	db.Close()
	if err = os.WriteFile(path+".wal", []byte("not a WAL; refusal fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(ctx, investigation.OfflineSelection{DB: path, System: "shop", Interval: time.Second}); err == nil {
		t.Fatal("WAL source accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = Open(cancelled, investigation.OfflineSelection{DB: empty, System: "shop", Interval: time.Second}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestOfflineRejectsUnrepresentableReference(t *testing.T) {
	for _, at := range []time.Time{time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC)} {
		ctx := context.Background()
		path := closedFixture(t)
		st, err := store.OpenPath(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = st.DB().Exec(`UPDATE epochs SET started_at=?,finished_at=?`, at, at); err != nil {
			t.Fatal(err)
		}
		st.Close()
		session, err := Open(ctx, investigation.OfflineSelection{DB: path, System: "shop", Interval: time.Second})
		if err == nil {
			session.Close()
			t.Fatalf("unrepresentable raw/derived reference accepted: %s", at)
		}
		var diagnostic *investigation.Diagnostic
		if !errors.As(err, &diagnostic) || diagnostic.Code != "no_reference_clock" {
			t.Fatalf("clock error not explicit: %v", err)
		}
	}
}

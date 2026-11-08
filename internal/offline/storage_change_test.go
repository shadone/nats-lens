package offline

import (
	"context"
	"fmt"
	"github.com/shadone/nats-lens/internal/investigation"
	"github.com/shadone/nats-lens/internal/store"
	"strings"
	"testing"
	"time"
)

func TestOfflineStorageChangeZeroDurationReference(t *testing.T) {
	ctx := context.Background()
	path := closedFixture(t)
	st, err := store.OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.DB().Exec(`UPDATE epochs SET finished_at=started_at,duration_ms=0`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	session, err := Open(ctx, investigation.OfflineSelection{DB: path, System: "shop", Interval: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	request := fmt.Sprintf(`{"system":"shop","a":"2026-10-17T14:28:00Z","b":%q}`, session.QueryReference.Format(time.RFC3339Nano))
	result, err := session.Source.Query(ctx, "GetStorageChange", []byte(request))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result), `"epoch":"9007199254740993"`) {
		t.Fatalf("latest completed sample lost: %s", result)
	}
	if !strings.Contains(string(result), `exclusive_end_inclusion`) {
		t.Fatal("derived exclusive bound was not disclosed")
	}
	if strings.Contains(string(result), `/capacity/storage-change?`) {
		t.Fatal("offline response advertises an unavailable remote UI link")
	}
}

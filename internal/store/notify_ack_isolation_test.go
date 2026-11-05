package store

import (
	"context"
	"testing"
	"time"
)

func TestSecondReviewBackfillReleaseBeforeChannelBaseline(t *testing.T) {
	db, _ := openAllButLast(t)
	at := t0.Add(time.Hour)
	if _, err := db.Exec(`INSERT INTO notify_channels VALUES ('new',?,2,?,2)`, at, at); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO notify_state VALUES ('new','stream-001:APP/jobs',1,'warning','firing',?,2)`, at); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO acknowledgements (id,finding_id,check_code,entity_type,entity_key,since_epoch,acked_by,created_at,ended_at,end_reason) VALUES (1,'stream-001:APP/jobs','stream-001','stream','APP/jobs',1,'operator',?,?,'released')`, t0, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var got bool
	if err := db.QueryRow(`SELECT acknowledged FROM notify_state WHERE channel='new'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("backfill marked channel acknowledged although release predates its baseline")
	}
}

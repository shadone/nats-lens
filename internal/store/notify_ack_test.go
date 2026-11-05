package store

import (
	"context"
	"testing"
	"time"
)

func TestNotifyAcknowledgementMemoryUpgrade(t *testing.T) {
	db, all := openAllButLast(t)
	ctx := context.Background()
	at := t0.Add(123456 * time.Microsecond)
	for _, name := range []string{"known", "older-cursor", "different-episode", "no-history"} {
		cursor := at
		if name == "older-cursor" {
			cursor = at.Add(-time.Minute)
		}
		if _, err := db.Exec(`INSERT INTO notify_channels VALUES (?, ?, 1, ?, 2)`, name, at, cursor); err != nil {
			t.Fatal(err)
		}
		episode := 1
		if name == "different-episode" {
			episode = 2
		}
		id := "stream-001:APP/jobs"
		if name == "no-history" {
			id = "stream-001:APP/other"
		}
		if _, err := db.Exec(`INSERT INTO notify_state VALUES (?, ?, ?, 'warning', 'flapping', ?, 2)`, name, id, episode, at); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO acknowledgements (id, finding_id, check_code, entity_type, entity_key, since_epoch, acked_by, created_at, ended_at, end_reason)
VALUES (1, 'stream-001:APP/jobs', 'stream-001', 'stream', 'APP/jobs', 1, 'operator', ?, ?, 'released')`, at, at); err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if !versionApplied(t, db, all[len(all)-1].version) {
		t.Fatal("upgrade not applied")
	}
	rows, err := db.Query(`SELECT channel, acknowledged FROM notify_state ORDER BY channel`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var channel string
		var acknowledged bool
		if err := rows.Scan(&channel, &acknowledged); err != nil {
			t.Fatal(err)
		}
		if acknowledged != (channel == "known") {
			t.Errorf("%s acknowledged = %t", channel, acknowledged)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

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

// Cursor overlap cannot establish that this channel saw the acknowledgement.
// Upgrade inference uses a state observation, or its actual retained message.
func TestNotifyAcknowledgementUpgradeEvidence(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		created, ended, observed        time.Duration
		messageChannel, key, kind, body string
		noHistory, want                 bool
	}{
		{name: "active-history-without-view-evidence", created: time.Minute, ended: 3 * time.Minute, observed: 2 * time.Minute},
		{name: "created-at-observation-with-firing", created: 2 * time.Minute, observed: 2 * time.Minute, messageChannel: "ops", kind: "firing", key: "example-prod:stream-001:APP/jobs:1:firing:3", want: true},
		{name: "active-history-with-firing", created: time.Minute, ended: 3 * time.Minute, observed: 2 * time.Minute, messageChannel: "ops", kind: "firing", key: "example-prod:stream-001:APP/jobs:1:firing:3", want: true},
		{name: "release-at-observation", created: time.Minute, ended: 2 * time.Minute, observed: 2 * time.Minute},
		{name: "release-before-baseline", created: -time.Minute, ended: -time.Second, observed: 0},
		{name: "entered-after-release", created: time.Minute, ended: 2 * time.Minute, observed: 3 * time.Minute},
		{name: "cursor-passed-note-but-no-observation", created: 2 * time.Minute, ended: 3 * time.Minute, observed: time.Minute},
		{name: "not-yet-created", created: 5 * time.Minute, observed: time.Minute},
		{name: "own-notified-note", noHistory: true, observed: 2 * time.Minute, messageChannel: "ops", kind: "acknowledged", key: "example-prod:stream-001:APP/jobs:1:acknowledged:3:1", want: true},
		{name: "other-channel-note", noHistory: true, observed: 2 * time.Minute, messageChannel: "other", kind: "acknowledged", key: "example-prod:stream-001:APP/jobs:1:acknowledged:3:1"},
		{name: "other-episode-note", noHistory: true, observed: 2 * time.Minute, messageChannel: "ops", kind: "acknowledged", key: "example-prod:stream-001:APP/jobs:2:acknowledged:3:1"},
		{name: "own-quiet-end", noHistory: true, observed: 2 * time.Minute, messageChannel: "ops", kind: "reminder", body: `{"reason":"acknowledgement quiet ended"}`, key: "example-prod:stream-001:APP/jobs:1:reminder:3", want: true},
		{name: "ordinary-reminder", noHistory: true, observed: 2 * time.Minute, messageChannel: "ops", kind: "reminder", key: "example-prod:stream-001:APP/jobs:1:reminder:3"},
		{name: "old-note-before-possible-reentry", noHistory: true, observed: 3 * time.Minute, messageChannel: "ops", kind: "acknowledged", key: "example-prod:stream-001:APP/jobs:1:acknowledged:3:1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := openAllButLast(t)
			at := t0.Add(time.Hour)
			if _, err := db.Exec(`INSERT INTO notify_channels VALUES ('ops',?,1,?,5)`, at, at.Add(4*time.Minute)); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO notify_state VALUES ('ops','stream-001:APP/jobs',1,'warning','flapping',?,3)`, at.Add(tc.observed)); err != nil {
				t.Fatal(err)
			}
			if !tc.noHistory {
				var ended any
				if tc.ended != 0 {
					ended = at.Add(tc.ended)
				}
				if _, err := db.Exec(`INSERT INTO acknowledgements (id,finding_id,check_code,entity_type,entity_key,since_epoch,acked_by,created_at,ended_at) VALUES (1,'stream-001:APP/jobs','stream-001','stream','APP/jobs',1,'operator',?,?)`, at.Add(tc.created), ended); err != nil {
					t.Fatal(err)
				}
			}
			if tc.kind != "" {
				body := tc.body
				if body == "" {
					body = "{}"
				}
				if _, err := db.Exec(`INSERT INTO notify_outbox (id,channel,created_at,epoch,kind,finding_id,dedup_key,message) VALUES (1,?,?,3,?,'stream-001:APP/jobs',?,?)`, tc.messageChannel, at.Add(2*time.Minute), tc.kind, tc.key, body); err != nil {
					t.Fatal(err)
				}
			}
			if err := migrate(context.Background(), db); err != nil {
				t.Fatal(err)
			}
			var got bool
			if err := db.QueryRow(`SELECT acknowledged FROM notify_state WHERE channel='ops'`).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("upgrade memory = %v, want %v", got, tc.want)
			}
			if err := migrate(context.Background(), db); err != nil {
				t.Fatal("reopen migration", err)
			}
		})
	}
}

func TestNotifyAcknowledgementUpgradeBaselineBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name           string
		created, ended time.Duration
		want           bool
	}{
		{"created-before-baseline", -time.Minute, time.Minute, true},
		{"created-at-baseline", 0, time.Minute, true},
		{"released-at-baseline", -time.Minute, 0, false},
		{"released-before-baseline", -time.Minute, -time.Second, false},
		{"created-after-baseline", time.Minute, 2 * time.Minute, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := openAllButLast(t)
			at := t0.Add(time.Hour)
			if _, err := db.Exec(`INSERT INTO notify_channels VALUES ('ops',?,1,?,5)`, at, at.Add(4*time.Minute)); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO notify_state VALUES ('ops','stream-001:APP/jobs',1,'warning','firing',?,1)`, at); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO acknowledgements (id,finding_id,check_code,entity_type,entity_key,since_epoch,acked_by,created_at,ended_at,quiet_until) VALUES (1,'stream-001:APP/jobs','stream-001','stream','APP/jobs',1,'operator',?,?,?)`, at.Add(tc.created), at.Add(tc.ended), at.Add(-time.Second)); err != nil {
				t.Fatal(err)
			}
			if err := migrate(context.Background(), db); err != nil {
				t.Fatal(err)
			}
			var got bool
			if err := db.QueryRow(`SELECT acknowledged FROM notify_state WHERE channel='ops'`).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("baseline memory=%v, want %v", got, tc.want)
			}
		})
	}
}

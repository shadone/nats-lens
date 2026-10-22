package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/micro"

	"github.com/shadone/nats-lens/internal/collect"
)

// rowStrings runs q and returns each row's columns joined by "|", NULL as
// "NULL".
func rowStrings(t *testing.T, db *sql.DB, q string, args ...any) []string {
	t.Helper()
	rows, err := db.Query(q, args...)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		parts := make([]string, len(vals))
		for i, v := range vals {
			parts[i] = "NULL"
			if v.Valid {
				parts[i] = v.String
			}
		}
		out = append(out, strings.Join(parts, "|"))
	}
	return out
}

func wantRows(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s\n got %q\nwant %q", what, got, want)
	}
}

// Every request is one micro_requests row with its state and error; INFO
// replies are micro_instances rows, STATS replies micro_stats rows and
// one micro_endpoints row per endpoint. What a service did not report is
// NULL.
func TestMicroWritten(t *testing.T) {
	s := openMem(t)
	epoch, err := s.WriteSnapshot(context.Background(), fixtureSnapshot(t, t0))
	if err != nil {
		t.Fatal(err)
	}
	db := s.DB()
	wantRows(t, "micro_requests", rowStrings(t, db, `SELECT label, account, request, state, replies, error
FROM micro_requests WHERE epoch = ? ORDER BY label, request`, epoch), []string{
		"APP|AFAKEAPPACCOUNT|INFO|ok|2|NULL",
		"APP|AFAKEAPPACCOUNT|STATS|ok|2|NULL",
		`OPS|OPS|INFO|error|0|nats: permissions violation: Permissions Violation for Publish to "$SRV.INFO"`,
		`OPS|OPS|STATS|error|0|nats: permissions violation: Permissions Violation for Publish to "$SRV.STATS"`,
	})
	wantRows(t, "micro_instances", rowStrings(t, db, `SELECT account, service, instance_id, version, description,
  metadata::VARCHAR, json_extract_string(endpoints, '$[1].metadata.cache'), json_array_length(endpoints)
FROM micro_instances WHERE epoch = ? ORDER BY instance_id`, epoch), []string{
		"AFAKEAPPACCOUNT|orders|ORD1|1.4.2|Order intake|NULL|on|2",
		"AFAKEAPPACCOUNT|orders|ORD2|1.4.2|Order intake|NULL|on|2",
	})
	wantRows(t, "micro_stats", rowStrings(t, db, `SELECT account, service, instance_id, version, strftime(started, '%H:%M')
FROM micro_stats WHERE epoch = ? ORDER BY instance_id`, epoch), []string{
		"AFAKEAPPACCOUNT|orders|ORD1|1.4.2|22:17",
		"AFAKEAPPACCOUNT|orders|ORD2|1.4.2|21:17",
	})
	wantRows(t, "micro_endpoints", rowStrings(t, db, `SELECT instance_id, endpoint, subject, queue_group, num_requests,
  num_errors, processing_time_ns, average_processing_time_ns, last_error, data::VARCHAR
FROM micro_endpoints WHERE epoch = ? ORDER BY instance_id, endpoint`, epoch), []string{
		"ORD1|create|orders.create|q|10|0|10000000|1000000|NULL|NULL",
		"ORD1|get|orders.get.*|q|100|0|50000000|500000|NULL|NULL",
		`ORD2|create|orders.create|q|20|1|20000000|1000000|409 duplicate order id|{"retries":3}`,
		"ORD2|get|orders.get.*|q|100|0|50000000|500000|NULL|NULL",
	})
}

// An instance without a version, description or endpoints, and an
// endpoint without a subject or queue group, store NULL and [], not
// empty strings; a snapshot without account connections writes nothing.
func TestMicroUnreportedValues(t *testing.T) {
	s := openMem(t)
	snap := fixtureSnapshot(t, t0)
	id := micro.ServiceIdentity{Name: "bare", ID: "B1"}
	snap.Micro = []collect.MicroAccount{{Label: "APP", Account: "APP",
		Asked: []collect.MicroAsked{{Request: collect.MicroInfo, State: collect.MicroOK, Replies: 1},
			{Request: collect.MicroStats, State: collect.MicroOK, Replies: 1}},
		Infos: []micro.Info{{ServiceIdentity: id, Type: micro.InfoResponseType}},
		Stats: []micro.Stats{{ServiceIdentity: id, Type: micro.StatsResponseType,
			Endpoints: []*micro.EndpointStats{{Name: "ping"}}}}}}
	epoch, err := s.WriteSnapshot(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	db := s.DB()
	wantRows(t, "micro_instances", rowStrings(t, db, `SELECT version, description, metadata::VARCHAR, endpoints::VARCHAR
FROM micro_instances WHERE epoch = ?`, epoch), []string{"NULL|NULL|NULL|[]"})
	wantRows(t, "micro_stats", rowStrings(t, db, `SELECT version, started FROM micro_stats WHERE epoch = ?`, epoch),
		[]string{"NULL|NULL"})
	wantRows(t, "micro_endpoints", rowStrings(t, db, `SELECT subject, queue_group, num_requests, last_error, data::VARCHAR
FROM micro_endpoints WHERE epoch = ?`, epoch), []string{"NULL|NULL|0|NULL|NULL"})

	snap = fixtureSnapshot(t, t0.Add(time.Minute))
	snap.Micro = nil
	epoch, err = s.WriteSnapshot(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range accountEpochTables {
		if n := count(t, db, `SELECT count(*) FROM `+table+` WHERE epoch = ?`, epoch); n != 0 {
			t.Errorf("%s has %d rows without account connections", table, n)
		}
	}
}

// Custom stats over 1 KB are stored as an object that gives their size;
// up to 1 KB they are stored as sent.
func TestMicroEndpointDataCapped(t *testing.T) {
	s := openMem(t)
	snap := fixtureSnapshot(t, t0)
	big := json.RawMessage(`{"blob":"` + strings.Repeat("x", 2000) + `"}`)
	snap.Micro[0].Stats[0].Endpoints[0].Data = big
	epoch, err := s.WriteSnapshot(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	wantRows(t, "data", rowStrings(t, s.DB(), `SELECT instance_id, data::VARCHAR FROM micro_endpoints
WHERE epoch = ? AND endpoint = 'create' ORDER BY instance_id`, epoch), []string{
		fmt.Sprintf(`ORD1|{"truncated":true,"bytes":%d}`, len(big)),
		`ORD2|{"retries":3}`,
	})
}

// bigMicroAccount is one account with services×instances instances of
// endpoints endpoints each, as INFO and STATS replies.
func bigMicroAccount(start time.Time, services, instances, endpoints int) collect.MicroAccount {
	acc := collect.MicroAccount{Label: "APP", Account: "APP", Asked: []collect.MicroAsked{
		{Request: collect.MicroInfo, State: collect.MicroOK, Replies: services * instances},
		{Request: collect.MicroStats, State: collect.MicroOK, Replies: services * instances}}}
	for sv := range services {
		for in := range instances {
			id := micro.ServiceIdentity{Name: fmt.Sprintf("svc%02d", sv), ID: fmt.Sprintf("I%03d", in), Version: "1.0.0"}
			info := micro.Info{ServiceIdentity: id, Type: micro.InfoResponseType, Description: "load"}
			stats := micro.Stats{ServiceIdentity: id, Type: micro.StatsResponseType, Started: start.Add(-time.Hour)}
			for e := range endpoints {
				name := fmt.Sprintf("op%02d", e)
				subject := fmt.Sprintf("shop.%s.%s", id.Name, name)
				info.Endpoints = append(info.Endpoints, micro.EndpointInfo{Name: name, Subject: subject, QueueGroup: "q"})
				stats.Endpoints = append(stats.Endpoints, &micro.EndpointStats{Name: name, Subject: subject, QueueGroup: "q",
					NumRequests: 1000 + e, NumErrors: e, ProcessingTime: time.Second, AverageProcessingTime: time.Millisecond})
			}
			acc.Infos = append(acc.Infos, info)
			acc.Stats = append(acc.Stats, stats)
		}
	}
	return acc
}

// 100 instances of 20 endpoints each are 100 micro_instances and
// micro_stats rows and 2,000 micro_endpoints rows, written in one epoch.
func TestMicroManyInstances(t *testing.T) {
	s := openMem(t)
	snap := fixtureSnapshot(t, t0)
	snap.Micro = nil
	start := time.Now()
	if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	t.Logf("epoch without micro services written in %s", time.Since(start))
	snap = fixtureSnapshot(t, t0.Add(time.Minute))
	snap.Micro = []collect.MicroAccount{bigMicroAccount(t0, 1, 100, 20)}
	start = time.Now()
	epoch, err := s.WriteSnapshot(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("epoch with 100 instances x 20 endpoints written in %s", time.Since(start))
	db := s.DB()
	if n := count(t, db, `SELECT count(*) FROM micro_endpoints WHERE epoch = ?`, epoch); n != 2000 {
		t.Errorf("micro_endpoints = %d, want 2000", n)
	}
	if n := count(t, db, `SELECT sum(num_errors) FROM micro_endpoints WHERE epoch = ?`, epoch); n != 100*190 {
		t.Errorf("errors summed to %d", n)
	}
	if n := count(t, db, `SELECT count(*) FROM micro_instances WHERE epoch = ?`, epoch); n != 100 {
		t.Errorf("micro_instances = %d, want 100", n)
	}
}

// The micro tables go with their epoch in a sweep.
func TestMicroSwept(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	for i := range 2 {
		if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(time.Duration(i)*24*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Sweep(ctx, t0.Add(24*time.Hour+time.Minute), time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, table := range accountEpochTables {
		if n := count(t, s.DB(), `SELECT count(*) FROM `+table+` WHERE epoch = 1`); n != 0 {
			t.Errorf("swept epoch keeps %d %s rows", n, table)
		}
		if n := count(t, s.DB(), `SELECT count(*) FROM `+table+` WHERE epoch = 2`); n == 0 {
			t.Errorf("kept epoch lost its %s rows", table)
		}
	}
}

// Migration 0033 applies to a database written by 0001 to 0032 that holds
// rows; its tables start empty and take rows, and micro_requests takes one
// row per epoch, label and request.
func TestMigrationsFrom32OnAPopulatedDatabase(t *testing.T) {
	ctx := context.Background()
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("duckdb", filepath.Join(t.TempDir(), "db.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE migrations (version INTEGER PRIMARY KEY, name VARCHAR NOT NULL, applied_at TIMESTAMP NOT NULL DEFAULT current_timestamp)`); err != nil {
		t.Fatal(err)
	}
	if err := migrateWith(ctx, db, all[:32], 0); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO server_stats (epoch, server_id, subscriptions) VALUES (1, 'NSERVER0', 40)`,
		`INSERT INTO subscriptions VALUES (1, 'NSERVER0', 'APP', '$SRV.STATS', NULL, 7, 1, 0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := migrateWith(ctx, db, all, 32); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM micro_requests) + (SELECT count(*) FROM micro_instances)
  + (SELECT count(*) FROM micro_stats) + (SELECT count(*) FROM micro_endpoints)`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("new tables hold %d rows, %v", n, err)
	}
	for _, q := range []string{
		`INSERT INTO micro_requests VALUES (1, 'APP', 'APP', 'STATS', 'ok', 1, NULL)`,
		`INSERT INTO micro_instances VALUES (1, 'APP', 'orders', 'ORD1', '1.4.2', NULL, NULL, '[]')`,
		`INSERT INTO micro_stats VALUES (1, 'APP', 'orders', 'ORD1', '1.4.2', NULL)`,
		`INSERT INTO micro_endpoints VALUES (1, 'APP', 'orders', 'ORD1', 'get', 'orders.get.*', 'q', 1, 0, 5, 5, NULL, NULL)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO micro_requests VALUES (1, 'APP', 'APP', 'STATS', 'timeout', 0, NULL)`); err == nil {
		t.Error("a second STATS row for one label and epoch was accepted")
	}
}

package store

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// jsServers are the scenario's servers; the first ID needs quoting in SQL.
var jsServers = []struct{ id, name string }{{"NSERVER'0", "nats-0"}, {"NSERVER1", "nats-1"}, {"NSERVER2", "nats-2"}}

// oddStream is a stream name that needs quoting in SQL and escaping in JSON.
const oddStream = `odd "name" 'q' \ back`

// quotedAccount is an account identifier that needs quoting in SQL.
const quotedAccount = `ACC 'q" \`

var (
	jsNanos   = time.Date(2026, 10, 16, 23, 17, 46, 123456789, time.FixedZone("IST", 5*3600+1800))
	jsPre1970 = time.Date(1969, 12, 31, 23, 59, 59, 999999999, time.UTC)
)

// jsEpochs is the scenario's epochs in order; 6 is skipped, so epoch 7
// follows 5 as the previous stored epoch.
var jsEpochs = []int64{1, 2, 3, 4, 5, 7}

func peerList(name, group string, holders ...string) *server.ClusterInfo {
	since := t0.Add(-time.Hour)
	ci := &server.ClusterInfo{Name: "example-dev", RaftGroup: group, Leader: holders[0], LeaderSince: &since}
	for _, h := range holders {
		if h != name {
			ci.Replicas = append(ci.Replicas, &server.PeerInfo{Name: h, Current: true, Active: 250 * time.Millisecond,
				Lag: 2, Peer: "peer-" + h})
		}
	}
	return ci
}

// jsScenarioView is server i's JSZ at epoch e:
//
//   - orders, R3 on all three servers, each reporting another creation time,
//     with consumers worker and audit. Its
//     description changes on nats-0 in epoch 3 and reverts in 4; worker's ack
//     wait changes everywhere in 5; audit is created again on nats-2 in 5
//     with the same configuration.
//   - oddStream, R1 on nats-0 without cluster info, with counters above
//     2^53 and beyond int64, times with nanoseconds and before 1970, and a
//     consumer without configuration. It is gone in epoch 4 and created
//     again in 5.
//   - mirror, R2 on nats-0 and nats-1 without a leader, with a mirror error,
//     sources (one null), placement, an offline, pending replica and a null one.
//   - plain, on nats-2 only, without configuration, with a creation time in
//     epoch 1 only.
//   - nats-1 lists orders a second time with another configuration, which
//     the writer keeps out and notes. nats-1 does not answer in epoch 3.
//   - with bulk, nats-2 also holds bulk, R1, with more consumers than one
//     batch holds, in epochs 1 and 2.
func jsScenarioView(i int, e int64, bulk bool) *collect.ServerView {
	srv := jsServers[i]
	if i == 1 && e == 3 {
		return nil
	}
	all := []string{"nats-0", "nats-1", "nats-2"}
	app := &server.AccountDetail{Name: "APP", Id: "AAPP"}

	ordersCfg := func(desc string) *server.StreamConfig {
		return &server.StreamConfig{Name: "orders", Description: desc, Subjects: []string{"orders.>"},
			Retention: server.LimitsPolicy, Storage: server.FileStorage, Replicas: 3, MaxConsumers: -1, MaxMsgs: -1,
			MaxBytes: 1 << 40, MaxAge: 24 * time.Hour, MaxMsgsPer: -1, MaxMsgSize: -1, Discard: server.DiscardOld,
			Duplicates: 2 * time.Minute}
	}
	desc := "orders"
	if i == 0 && e == 3 {
		desc = "orders, changed"
	}
	ackWait := 30 * time.Second
	if e >= 5 {
		ackWait = time.Minute
	}
	auditCreated := t0.Add(-time.Hour)
	if i == 2 && e >= 5 {
		auditCreated = t0.Add(-time.Minute)
	}
	orders := server.StreamDetail{Name: "orders", Created: t0.Add(-48*time.Hour + time.Duration(i)*time.Second), Cluster: peerList(srv.name, "S-orders", all...),
		Config: ordersCfg(desc),
		State: server.StreamState{Msgs: 100 + uint64(e), Bytes: 1 << 20, FirstSeq: 1, LastSeq: 100 + uint64(e),
			FirstTime: t0.Add(-48 * time.Hour), LastTime: t0, NumSubjects: 3, Consumers: 2},
		Consumer: []*server.ConsumerInfo{
			{Stream: "orders", Name: "worker", Created: t0.Add(-2 * time.Hour), Cluster: peerList(srv.name, "C-worker", all...),
				Config: &server.ConsumerConfig{Durable: "worker", AckPolicy: server.AckExplicit, AckWait: ackWait,
					MaxDeliver: 5, BackOff: []time.Duration{time.Second, 5 * time.Second}, FilterSubject: "orders.new",
					FilterSubjects: []string{"orders.paid"}, MaxAckPending: 1000, MaxWaiting: 512},
				Delivered:     server.SequenceInfo{Consumer: 90, Stream: 95, Last: &jsNanos},
				AckFloor:      server.SequenceInfo{Consumer: 80, Stream: 85, Last: &jsNanos},
				NumAckPending: 10, NumRedelivered: 1, NumWaiting: 3, NumPending: 5, PushBound: true, TimeStamp: jsNanos},
			{Stream: "orders", Name: "audit", Created: auditCreated, Cluster: peerList(srv.name, "C-audit", all...),
				Config: &server.ConsumerConfig{Durable: "audit", DeliverSubject: "deliver.audit", DeliverGroup: "g",
					Heartbeat: 5 * time.Second, FlowControl: true, HeadersOnly: true, InactiveThreshold: time.Hour,
					Replicas: 1, MemoryStorage: true, Description: `line one
line "two"`},
				TimeStamp: t0},
		},
	}
	if i == 1 {
		dup := orders
		dup.Config = ordersCfg("orders as listed again")
		app.Streams = append(app.Streams, orders, dup)
	} else {
		app.Streams = append(app.Streams, orders)
	}

	if i == 0 && e != 4 {
		created := jsNanos.Add(-time.Hour)
		if e >= 5 {
			created = jsNanos
		}
		app.Streams = append(app.Streams, server.StreamDetail{Name: oddStream, Created: created,
			Config: &server.StreamConfig{Name: oddStream, Subjects: []string{"odd.>", `odd."q"`}, Retention: server.WorkQueuePolicy,
				Storage: server.MemoryStorage, Replicas: 1, MaxConsumers: math.MaxInt32, MaxMsgs: math.MaxInt64,
				MaxBytes: -1, MaxMsgsPer: above53, MaxMsgSize: math.MaxInt32, Discard: server.DiscardNew},
			State: server.StreamState{Msgs: above53, Bytes: math.MaxUint64, FirstSeq: math.MaxInt64, LastSeq: math.MaxUint64,
				FirstTime: jsPre1970, LastTime: jsNanos, NumSubjects: math.MaxInt32, NumDeleted: 7, Consumers: 2},
			Consumer: []*server.ConsumerInfo{
				{Stream: oddStream, Name: "edge", Created: jsPre1970,
					Config:     &server.ConsumerConfig{AckPolicy: server.AckNone, DeliverPolicy: server.DeliverLast, ReplayPolicy: server.ReplayOriginal},
					Delivered:  server.SequenceInfo{Consumer: math.MaxUint64, Stream: above53},
					AckFloor:   server.SequenceInfo{Consumer: above53, Stream: 1, Last: &jsPre1970},
					NumPending: above53, NumWaiting: math.MaxInt32, Paused: true},
				{Stream: oddStream, Name: "unconfigured"},
			},
		})
	}

	if i < 2 {
		mirrorCI := peerList(srv.name, "S-mirror", "nats-0", "nats-1")
		mirrorCI.Leader, mirrorCI.LeaderSince = "", nil
		mirrorCI.Replicas = append(mirrorCI.Replicas, nil, &server.PeerInfo{Name: "nats-9", Offline: true, Pending: true, Lag: math.MaxUint64, Active: -time.Second})
		app.Streams = append(app.Streams, server.StreamDetail{Name: "mirror", Created: t0.Add(-time.Hour), Cluster: mirrorCI,
			Config: &server.StreamConfig{Name: "mirror", Retention: server.InterestPolicy, Storage: server.FileStorage,
				Replicas: 2, Mirror: &server.StreamSource{Name: "orders"},
				Sources:     []*server.StreamSource{{Name: "orders"}, nil, {Name: "legacy-orders"}},
				Placement:   &server.Placement{Cluster: "example-dev", Tags: []string{"ssd", "zone:a"}},
				Compression: server.S2Compression, Sealed: true, DenyDelete: true, DenyPurge: true, AllowDirect: true,
				AllowMsgTTL: true},
			State:   server.StreamState{Msgs: 1, LastTime: jsNanos},
			Mirror:  &server.StreamSourceInfo{Name: "orders", Lag: above53, Active: 1500 * time.Millisecond, Error: &server.ApiError{Code: 503, Description: `no "responders"`}},
			Sources: []*server.StreamSourceInfo{{Name: "orders", Lag: 4}, nil, {Name: "legacy-orders", Lag: above53}},
		})
	}

	if i == 2 {
		var plainCreated time.Time
		if e == 1 {
			plainCreated = t0
		}
		app.Streams = append(app.Streams, server.StreamDetail{Name: "plain", Created: plainCreated,
			Cluster: peerList(srv.name, "S-plain", "nats-2"), State: server.StreamState{Msgs: 5}})
		if bulk && e <= 2 {
			s := server.StreamDetail{Name: "bulk", Created: t0, Config: &server.StreamConfig{Name: "bulk", Replicas: 1}}
			for j := range jsonBatch + 3 {
				s.Consumer = append(s.Consumer, &server.ConsumerInfo{Stream: "bulk", Name: fmt.Sprintf("b-%d", j),
					Created: t0.Add(time.Duration(j) * time.Microsecond), Delivered: server.SequenceInfo{Consumer: uint64(j)},
					Config: &server.ConsumerConfig{Durable: fmt.Sprintf("b-%d", j), MaxDeliver: j}})
			}
			app.Streams = append(app.Streams, s)
		}
	}
	return &collect.ServerView{
		Info: server.ServerInfo{ID: srv.id, Name: srv.name},
		Jsz:  &server.JSInfo{AccountDetails: []*server.AccountDetail{app, nil}},
	}
}

// writeJSScenario writes the scenario's epochs to the store at path, with
// the row-at-a-time JetStream and identity writes when byRow and the JSON
// batches otherwise, and returns the notes of every epoch.
// jsScenarioIdents records the accounts and raft groups server i reports
// at epoch e besides its JSZ: APP's name differs between servers (nats-1
// first, and nats-2's while nats-1 is missing in epoch 3), only nats-2 in
// epoch 1 marks SYS as the system account, an account whose identifier needs
// quoting gains a name in epoch 5, and a raft group seen only in RAFTZ in
// epoch 4 learns its stream in 5 and keeps it when RAFTZ alone reports it
// again in 7.
func jsScenarioIdents(w *writer, i int, e int64) {
	names := []string{"AAPP", "APP", "APP"}
	if e >= 3 {
		names[2] = "app-renamed"
	}
	w.ids.account("AAPP", names[i], false)
	w.ids.account("SYS", "SYS", i == 2 && e == 1)
	if i == 0 {
		quoted := quotedAccount
		name := quoted
		if e >= 5 {
			name = "quoted"
		}
		w.ids.account(quoted, name, false)
	}
	w.ids.raftGroup("_meta_")
	if e == 4 || e == 7 {
		w.ids.raftGroup("S-raft-only")
	}
	if e == 5 && i == 1 {
		w.ids.raftAsset("S-raft-only", "AAPP", "raft-only", "")
	}
}

func writeJSScenario(t *testing.T, path string, bulk bool, byRow bool) []string {
	t.Helper()
	ctx := context.Background()
	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var notes []string
	prev := int64(0)
	for _, e := range jsEpochs {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		w := newWriter(ctx, tx, e, collect.NewSnapshot(t0))
		w.prevEpoch = prev
		w.warned = map[assetKey]bool{}
		w.log = slog.New(slog.DiscardHandler)
		for i := range jsServers {
			v := jsScenarioView(i, e, bulk)
			if v == nil {
				continue
			}
			if byRow {
				w.writeJetStreamByRow(v)
			} else {
				w.writeJetStream(v)
			}
			jsScenarioIdents(w, i, e)
		}
		if byRow {
			w.writeIdentsByRow()
		} else {
			w.writeIdents()
		}
		w.closeStatements()
		if w.err != nil {
			_ = tx.Rollback()
			t.Fatalf("epoch %d: %v", e, w.err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		notes = append(notes, w.notes...)
		prev = e
	}
	return notes
}

var jsTables = []string{
	"stream_stats", "stream_replicas", "consumer_stats", "consumer_replicas", "stream_config", "consumer_config",
	"accounts", "streams", "consumers", "raft_groups",
}

// TestJetStreamRowsMatchRowByRow writes the scenario's epochs once with
// the JSON batches and once with one statement per row, and requires
// every table touched to hold the same rows, NULLs included, both ways.
// The configuration versions go through every case: unchanged, changed,
// reverted, gone and back, a server missing for an epoch, a new creation
// time with the same configuration, a skipped epoch number, and a
// repeated report. Under -short the bulk stream is left out.
func TestJetStreamRowsMatchRowByRow(t *testing.T) {
	dir := t.TempDir()
	bulk := !testing.Short()
	ref, got := filepath.Join(dir, "by-row.duckdb"), filepath.Join(dir, "batch.duckdb")
	refNotes := writeJSScenario(t, ref, bulk, true)
	gotNotes := writeJSScenario(t, got, bulk, false)
	if !slices.Equal(refNotes, gotNotes) || len(gotNotes) != len(jsEpochs)-1 {
		t.Errorf("notes = %q, want %q (one per epoch nats-1 answers)", gotNotes, refNotes)
	}

	s, err := OpenPath(context.Background(), got)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	db := s.DB()
	if _, err := db.Exec(`ATTACH ` + sqlString(ref) + ` AS ref (READ_ONLY)`); err != nil {
		t.Fatal(err)
	}
	for _, table := range jsTables {
		n := count(t, db, `SELECT count(*) FROM `+table)
		if n == 0 || n != count(t, db, `SELECT count(*) FROM ref.`+table) {
			t.Errorf("%s: %d rows, by row %d", table, n, count(t, db, `SELECT count(*) FROM ref.`+table))
		}
		for _, q := range []string{
			`SELECT * FROM ref.` + table + ` EXCEPT ALL SELECT * FROM main.` + table,
			`SELECT * FROM main.` + table + ` EXCEPT ALL SELECT * FROM ref.` + table,
		} {
			if d := count(t, db, `SELECT count(*) FROM (`+q+`)`); d != 0 {
				t.Errorf("%d rows differ: %s", d, q)
			}
		}
	}
	if bulk {
		if n := count(t, db, `SELECT count(*) FROM consumer_config WHERE stream = 'bulk' AND first_epoch = 1 AND last_epoch = 2`); n != jsonBatch+3 {
			t.Errorf("bulk consumer runs over epochs 1 to 2 = %d, want %d", n, jsonBatch+3)
		}
	}
	checkJSRuns(t, s)
	checkJSValues(t, s)
	checkJSIdents(t, s)
}

// checkJSIdents compares the identity rows with the scenario's.
func checkJSIdents(t *testing.T, s *Store) {
	t.Helper()
	db := s.DB()
	for _, c := range []struct {
		q    string
		args []any
	}{
		{`SELECT count(*) FROM accounts WHERE account = 'AAPP' AND account_name = 'APP' AND NOT is_system
  AND first_epoch = 1 AND last_epoch = 7`, nil},
		{`SELECT count(*) FROM accounts WHERE account = 'SYS' AND is_system AND last_epoch = 7`, nil},
		{`SELECT count(*) FROM accounts WHERE account = ? AND account_name = 'quoted' AND first_epoch = 1`, []any{quotedAccount}},
		{`SELECT count(*) FROM streams WHERE stream = 'orders' AND created = ?::TIMESTAMP AND first_epoch = 1 AND last_epoch = 7`,
			[]any{t0.Add(-48 * time.Hour).Format("2006-01-02 15:04:05")}},
		{`SELECT count(*) FROM streams WHERE stream = ? AND created = ?::TIMESTAMP AND first_epoch = 1 AND last_epoch = 7`,
			[]any{oddStream, jsNanos.UTC().Truncate(time.Microsecond).Format("2006-01-02 15:04:05.000000")}},
		{`SELECT count(*) FROM streams WHERE stream = 'plain' AND created = ?::TIMESTAMP AND last_epoch = 7`,
			[]any{t0.Format("2006-01-02 15:04:05")}},
		{`SELECT count(*) FROM consumers WHERE consumer = 'audit' AND created = ?::TIMESTAMP AND last_epoch = 7`,
			[]any{t0.Add(-time.Hour).Format("2006-01-02 15:04:05")}},
		{`SELECT count(*) FROM consumers WHERE consumer = 'edge' AND stream = ? AND first_epoch = 1 AND last_epoch = 7`, []any{oddStream}},
		{`SELECT count(*) FROM raft_groups WHERE group_id = '_meta_' AND kind = 'meta' AND account IS NULL AND last_epoch = 7`, nil},
		{`SELECT count(*) FROM raft_groups WHERE group_id = 'S-raft-only' AND kind = 'stream' AND account = 'AAPP'
  AND stream = 'raft-only' AND consumer IS NULL AND first_epoch = 4 AND last_epoch = 7`, nil},
		{`SELECT count(*) FROM raft_groups WHERE group_id = 'C-worker' AND kind = 'consumer' AND stream = 'orders'
  AND consumer = 'worker' AND last_epoch = 7`, nil},
	} {
		if n := count(t, db, c.q, c.args...); n != 1 {
			t.Errorf("%d rows, want 1: %s %v", n, c.q, c.args)
		}
	}
}

// checkJSRuns compares the configuration runs with the scenario's.
func checkJSRuns(t *testing.T, s *Store) {
	t.Helper()
	runs := func(q string, args ...any) string {
		rows, err := s.DB().Query(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := ""
		for rows.Next() {
			var first, last int64
			if err := rows.Scan(&first, &last); err != nil {
				t.Fatal(err)
			}
			out += fmt.Sprintf("%d-%d ", first, last)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	const stream = `SELECT first_epoch, last_epoch FROM stream_config WHERE server_id = ? AND stream = ? ORDER BY first_epoch`
	const consumer = `SELECT first_epoch, last_epoch FROM consumer_config WHERE server_id = ? AND consumer = ? ORDER BY first_epoch`
	for _, c := range []struct {
		q, server, name, want string
	}{
		{stream, "NSERVER'0", "orders", "1-2 3-3 4-7 "},
		{stream, "NSERVER1", "orders", "1-2 4-7 "},
		{stream, "NSERVER2", "orders", "1-7 "},
		{stream, "NSERVER'0", oddStream, "1-3 5-7 "},
		{stream, "NSERVER1", "mirror", "1-2 4-7 "},
		{stream, "NSERVER2", "plain", ""},
		{consumer, "NSERVER2", "worker", "1-4 5-7 "},
		{consumer, "NSERVER1", "worker", "1-2 4-4 5-7 "},
		{consumer, "NSERVER2", "audit", "1-4 5-7 "},
		{consumer, "NSERVER'0", "audit", "1-7 "},
		{consumer, "NSERVER'0", "edge", "1-3 5-7 "},
		{consumer, "NSERVER'0", "unconfigured", ""},
	} {
		if got := runs(c.q, c.server, c.name); got != c.want {
			t.Errorf("runs of %s on %s = %q, want %q", c.name, c.server, got, c.want)
		}
	}
	if got := runs(`SELECT first_epoch, last_epoch FROM stream_config WHERE server_id = 'NSERVER1' AND stream = 'orders'
  AND description = 'orders as listed again'`); got != "" {
		t.Errorf("the repeated report's configuration was stored: %s", got)
	}
}

// checkJSValues reads edge rows back and compares them with the values
// given, so that a fault both writes share is caught too.
func checkJSValues(t *testing.T, s *Store) {
	t.Helper()
	db := s.DB()
	us := func(tm time.Time) time.Time { return tm.Truncate(time.Microsecond).UTC() }

	var (
		messages, bytes, firstSeq, lastSeq, numSubjects int64
		created, firstTS, lastTS                        time.Time
		mirrorLag, sourcesCount                         *int64
		clusterNull                                     bool
		isLeader                                        bool
	)
	if err := db.QueryRow(`SELECT messages, bytes, first_seq, last_seq, num_subjects, created, first_ts, last_ts,
  cluster IS NULL, mirror_lag, sources_count, is_leader FROM stream_stats WHERE epoch = 1 AND stream = ?`, oddStream).Scan(
		&messages, &bytes, &firstSeq, &lastSeq, &numSubjects, &created, &firstTS, &lastTS, &clusterNull, &mirrorLag,
		&sourcesCount, &isLeader); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		col       string
		got, want int64
	}{
		{"messages", messages, above53}, {"bytes", bytes, math.MaxInt64}, {"first_seq", firstSeq, math.MaxInt64},
		{"last_seq", lastSeq, math.MaxInt64}, {"num_subjects", numSubjects, math.MaxInt32},
	} {
		if x.got != x.want {
			t.Errorf("stream_stats.%s = %d, want %d", x.col, x.got, x.want)
		}
	}
	for _, x := range []struct {
		col       string
		got, want time.Time
	}{
		{"created", created, jsNanos.Add(-time.Hour)}, {"first_ts", firstTS, jsPre1970}, {"last_ts", lastTS, jsNanos},
	} {
		if !x.got.Equal(us(x.want)) {
			t.Errorf("stream_stats.%s = %v, want %v", x.col, x.got.UTC(), us(x.want))
		}
	}
	if !firstTS.Equal(time.Date(1969, 12, 31, 23, 59, 59, 999999000, time.UTC)) {
		t.Errorf("first_ts before 1970 = %v, want the earlier microsecond", firstTS.UTC())
	}
	if mirrorLag != nil || sourcesCount != nil || !isLeader || !clusterNull {
		t.Errorf("stream without mirror, sources or cluster: mirror_lag %v, sources_count %v, is_leader %v, cluster NULL %v", mirrorLag, sourcesCount, isLeader, clusterNull)
	}

	var (
		mirrorErr, leader     string
		lag, maxLag, srcCount int64
		since                 *time.Time
	)
	if err := db.QueryRow(`SELECT mirror_error, mirror_lag, sources_max_lag, sources_count, leader, leader_since
FROM stream_stats WHERE epoch = 1 AND stream = 'mirror' AND server_id = 'NSERVER''0'`).Scan(
		&mirrorErr, &lag, &maxLag, &srcCount, &leader, &since); err != nil {
		t.Fatal(err)
	}
	if mirrorErr != `no "responders"` || lag != above53 || maxLag != above53 || srcCount != 2 || leader != "" || since != nil {
		t.Errorf("mirror stats = %q %d %d %d %q %v", mirrorErr, lag, maxLag, srcCount, leader, since)
	}
	var replicas, pending, offline int
	var peerLag int64
	if err := db.QueryRow(`SELECT count(*), count(*) FILTER (WHERE pending), count(*) FILTER (WHERE offline),
  max(lag) FROM stream_replicas WHERE epoch = 1 AND stream = 'mirror' AND server_id = 'NSERVER''0'`).Scan(
		&replicas, &pending, &offline, &peerLag); err != nil {
		t.Fatal(err)
	}
	if replicas != 2 || pending != 1 || offline != 1 || peerLag != math.MaxInt64 {
		t.Errorf("mirror replicas = %d, pending %d, offline %d, max lag %d; want 2, 1, 1 and MaxInt64", replicas, pending, offline, peerLag)
	}

	var subjects, sources, tags, config, mirror, plCluster, compression string
	var maxMsgsPer int64
	if err := db.QueryRow(`SELECT (SELECT subjects FROM stream_config WHERE stream = ? AND first_epoch = 1)::VARCHAR,
  sources::VARCHAR, placement_tags::VARCHAR, config::VARCHAR, mirror, placement_cluster, compression,
  (SELECT max_msgs_per_subject FROM stream_config WHERE stream = ? AND first_epoch = 1)
FROM stream_config WHERE stream = 'mirror' AND server_id = 'NSERVER''0' AND first_epoch = 1`, oddStream, oddStream).Scan(
		&subjects, &sources, &tags, &config, &mirror, &plCluster, &compression, &maxMsgsPer); err != nil {
		t.Fatal(err)
	}
	if subjects != `[odd.>, 'odd."q"']` || sources != `[orders, legacy-orders]` || tags != `[ssd, 'zone:a']` ||
		mirror != "orders" || plCluster != "example-dev" || compression != "s2" || maxMsgsPer != above53 {
		t.Errorf("stream_config = %s %s %s %q %q %q %d", subjects, sources, tags, mirror, plCluster, compression, maxMsgsPer)
	}
	if want := jsonText(jsScenarioView(0, 1, false).Jsz.AccountDetails[0].Streams[2].Config); config != want {
		t.Errorf("stream_config.config = %s, want %s", config, want)
	}

	var (
		delivered, numPending, numWaiting int64
		lastActive, reportedAt            *time.Time
		ackLast, cCreated                 time.Time
		paused                            bool
	)
	if err := db.QueryRow(`SELECT delivered_consumer_seq, num_pending, num_waiting, delivered_last_active, reported_at,
  ack_floor_last_active, created, paused FROM consumer_stats WHERE epoch = 1 AND consumer = 'edge'`).Scan(
		&delivered, &numPending, &numWaiting, &lastActive, &reportedAt, &ackLast, &cCreated, &paused); err != nil {
		t.Fatal(err)
	}
	if delivered != math.MaxInt64 || numPending != above53 || numWaiting != math.MaxInt32 || lastActive != nil ||
		reportedAt != nil || !ackLast.Equal(us(jsPre1970)) || !cCreated.Equal(us(jsPre1970)) || !paused {
		t.Errorf("edge consumer = %d %d %d %v %v %v %v %v", delivered, numPending, numWaiting, lastActive, reportedAt,
			ackLast.UTC(), cCreated.UTC(), paused)
	}
	var followers, leaders int
	if err := db.QueryRow(`SELECT count(*) FILTER (WHERE NOT is_leader AND num_pending IS NULL AND push_bound IS NULL),
  count(*) FILTER (WHERE is_leader AND num_pending = 5 AND push_bound AND delivered_last_active = ?::TIMESTAMP)
FROM consumer_stats WHERE epoch = 1 AND consumer = 'worker'`, us(jsNanos).Format("2006-01-02 15:04:05.000000")).Scan(
		&followers, &leaders); err != nil {
		t.Fatal(err)
	}
	if followers != 2 || leaders != 1 {
		t.Errorf("worker rows: %d followers with NULL leader values, %d leader rows; want 2 and 1", followers, leaders)
	}
	var backoff, filters, desc string
	var ackWait int64
	if err := db.QueryRow(`SELECT backoff_ns::VARCHAR, filter_subjects::VARCHAR, ack_wait_ns,
  (SELECT description FROM consumer_config WHERE consumer = 'audit' LIMIT 1)
FROM consumer_config WHERE consumer = 'worker' AND server_id = 'NSERVER2' AND first_epoch = 5`).Scan(
		&backoff, &filters, &ackWait, &desc); err != nil {
		t.Fatal(err)
	}
	if backoff != "[1000000000, 5000000000]" || filters != "[orders.new, orders.paid]" || ackWait != int64(time.Minute) ||
		desc != "line one\nline \"two\"" {
		t.Errorf("consumer_config = %s %s %d %q", backoff, filters, ackWait, desc)
	}
}

// TestJetStreamWriteErrorStopsTheEpoch gives a value that does not fit
// its column, once in the stats and once in a configuration: the writer
// keeps the error, so that WriteSnapshot rolls the epoch back, and the
// next server's write does not replace it.
func TestJetStreamWriteErrorStopsTheEpoch(t *testing.T) {
	for _, c := range []struct {
		name  string
		spoil func(*collect.ServerView)
		want  string
	}{
		{"stats", func(v *collect.ServerView) { v.Jsz.AccountDetails[0].Streams[0].State.Consumers = math.MaxInt32 + 1 }, "stream_stats"},
		{"config", func(v *collect.ServerView) {
			v.Jsz.AccountDetails[0].Streams[0].Config.MaxConsumers = math.MaxInt32 + 1
		}, "stage_stream_config"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := openMem(t)
			ctx := context.Background()
			tx, err := s.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			w := newWriter(ctx, tx, 1, collect.NewSnapshot(t0))
			v := jsScenarioView(2, 1, false)
			c.spoil(v)
			w.writeJetStream(v)
			w.writeJetStream(jsScenarioView(0, 1, false))
			if w.err == nil || !strings.Contains(w.err.Error(), c.want) {
				t.Fatalf("err = %v, want an error naming %s", w.err, c.want)
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if n := count(t, s.DB(), `SELECT count(*) FROM stream_stats`); n != 0 {
				t.Errorf("%d stream_stats rows after the rollback", n)
			}
		})
	}
}

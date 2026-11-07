package collect_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/collect"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Capture real replies, then replay complete reply sets on a broker with no
// system account. The selected JSZ views stay stale by construction; no sleep
// or chance of catching a replica between apply operations determines the test.
func TestLastR3DeleteRaftzReplyBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("starts disposable loopback servers")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cluster := testcluster.Start(t, 3)
	sys := cluster.Connect(0, testcluster.SysUser, testcluster.SysPass)
	js, err := jetstream.New(cluster.Connect(0, testcluster.AppUser, testcluster.AppPass))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: "catalog-changes", Subjects: []string{"catalog.changes.>"}, Replicas: 3}); err != nil {
		t.Fatal(err)
	}
	waitStreamOnEveryServer(t, cluster, "catalog-changes", true)
	ids := []string{cluster.Servers[0].ID(), cluster.Servers[1].ID(), cluster.Servers[2].ID()}
	endpoints := append([]collect.Endpoint{collect.Statsz}, collect.DetailEndpoints...)
	endpoints = append(endpoints, collect.AccountRaftz("APP"))
	before := map[collect.Endpoint]map[string][]byte{}
	for _, e := range endpoints {
		before[e] = captureMonitoring(t, ctx, sys, e, ids)
	}
	for _, id := range ids {
		var r server.ServerAPIRaftzResponse
		if err := json.Unmarshal(before[collect.AccountRaftz("APP")][id], &r); err != nil {
			t.Fatal(err)
		}
		if r.Error != nil || r.Data == nil || len((*r.Data)["APP"]) == 0 {
			t.Fatalf("before R3 delete %s: %s", id, before[collect.AccountRaftz("APP")][id])
		}
	}
	if err := js.DeleteStream(ctx, "catalog-changes"); err != nil {
		t.Fatal(err)
	}
	waitStreamOnEveryServer(t, cluster, "catalog-changes", false)
	afterJSZ := captureMonitoring(t, ctx, sys, collect.Jsz, ids)
	afterRaft := captureMonitoring(t, ctx, sys, collect.AccountRaftz("APP"), ids)
	for _, id := range ids {
		var r server.ServerAPIRaftzResponse
		if err := json.Unmarshal(afterRaft[id], &r); err != nil {
			t.Fatal(err)
		}
		if r.Error != nil || r.Data == nil || len(*r.Data) != 0 {
			t.Fatalf("after real R3 delete %s: %s", id, afterRaft[id])
		}
	}
	t.Log("real R3 deletion: all three account RAFTZ requests answered with data:{}")

	broker, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true, NoSystemAccount: true})
	if err != nil {
		t.Fatal(err)
	}
	broker.Start()
	t.Cleanup(func() { broker.Shutdown(); broker.WaitForShutdown() })
	if !broker.ReadyForConnections(5 * time.Second) {
		t.Fatal("replay broker not ready")
	}
	nc, err := nats.Connect(broker.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	type frame struct {
		stale                                        int
		missingJSZ, errorJSZ, missingRaft, errorRaft bool
	}
	var current atomic.Pointer[frame]
	current.Store(&frame{stale: 3})
	_, err = nc.Subscribe("$SYS.REQ.>", func(m *nats.Msg) {
		f := current.Load()
		var e collect.Endpoint
		switch m.Subject {
		case collect.AccountStatz.Subject():
			e = collect.AccountStatz
		default:
			e = collect.Endpoint(strings.TrimPrefix(m.Subject, "$SYS.REQ.SERVER.PING."))
		}
		if e == collect.Raftz {
			var opts server.RaftzOptions
			_ = json.Unmarshal(m.Data, &opts)
			if opts.AccountFilter != "" {
				e = collect.AccountRaftz(opts.AccountFilter)
			}
		}
		for i, id := range ids {
			body := before[e][id]
			if e == collect.Jsz {
				if i == 0 && f.missingJSZ {
					continue
				}
				if i < len(ids)-f.stale {
					body = afterJSZ[id]
				}
				if i == 0 && f.errorJSZ {
					body = monitoringError(id, cluster.Names[i])
				}
			}
			if e == collect.AccountRaftz("APP") {
				body = afterRaft[id]
				if i == 0 && f.missingRaft {
					continue
				}
				if i == 0 && f.errorRaft {
					body = monitoringError(id, cluster.Names[i])
				}
			}
			if len(body) > 0 {
				_ = m.Respond(body)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	col := newCol(nc, cluster.Names...)
	for _, tc := range []struct {
		name     string
		f        frame
		want     collect.State
		partial  bool
		failures int
	}{
		{"stable", frame{stale: 3}, collect.StateOK, false, 0},
		{"two servers still list account", frame{stale: 2}, collect.StateOK, false, 0},
		{"one server still lists account", frame{stale: 1}, collect.StateOK, false, 0},
		// Even a server whose successful JSZ no longer lists streams owes a
		// reply: empty RAFTZ is a real answer, silence is not negative evidence.
		{"absent locally but RAFTZ lost", frame{stale: 1, missingRaft: true}, collect.StateMissing, true, 1},
		{"absent locally but RAFTZ error", frame{stale: 1, errorRaft: true}, collect.StateError, true, 1},
		{"JSZ unknown and RAFTZ lost", frame{missingJSZ: true, missingRaft: true}, collect.StateMissing, true, 2},
		{"JSZ failed and RAFTZ lost", frame{errorJSZ: true, missingRaft: true}, collect.StateMissing, true, 2},
		{"JSZ failed but RAFTZ answered", frame{errorJSZ: true}, collect.StateOK, true, 1},
		{"all gone despite RAFTZ lost", frame{missingRaft: true}, "", false, 0},
		{"all gone", frame{}, "", false, 0},
		{"account reappears", frame{stale: 3}, collect.StateOK, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current.Store(&tc.f)
			snap := col.Collect(ctx)
			for _, id := range ids {
				got, ok := snap.Servers[id].Status[collect.AccountRaftz("APP")]
				want := tc.want
				if id != ids[0] && want != "" {
					want = collect.StateOK
				}
				if (want != "") != ok || got.State != want {
					t.Errorf("%s RAFTZ:APP = %+v, present=%v; want %s", id, got, ok, want)
				}
				if got := snap.Servers[id].Status[collect.Raftz].State; got != collect.StateOK {
					t.Errorf("meta RAFTZ = %s", got)
				}
			}
			if snap.Partial() != tc.partial {
				t.Errorf("partial=%v errors=%v", snap.Partial(), snap.Errors)
				for _, v := range snap.Servers {
					for e, status := range v.Status {
						if status.State != collect.StateOK {
							t.Logf("%s %s %+v", v.Info.Name, e, status)
						}
					}
				}
			}
			epoch, err := st.WriteSnapshot(ctx, snap)
			if err != nil {
				t.Fatal(err)
			}
			var partial bool
			var failures, rows, bad, groups int
			if err := st.DB().QueryRowContext(ctx, `SELECT partial, endpoint_failures FROM epochs WHERE epoch = ?`, epoch).Scan(&partial, &failures); err != nil {
				t.Fatal(err)
			}
			if err := st.DB().QueryRowContext(ctx, `SELECT count(*), count(*) FILTER (WHERE state <> 'ok') FROM epoch_endpoints WHERE epoch = ? AND endpoint = 'RAFTZ:APP'`, epoch).Scan(&rows, &bad); err != nil {
				t.Fatal(err)
			}
			if err := st.DB().QueryRowContext(ctx, `SELECT count(*) FROM raft_stats WHERE epoch = ? AND raft_account = 'APP'`, epoch).Scan(&groups); err != nil {
				t.Fatal(err)
			}
			wantRows := 3
			if tc.want == "" {
				wantRows = 0
			}
			wantBad := 0
			if wantRows > 0 && (tc.f.missingRaft || tc.f.errorRaft) {
				wantBad = 1
			}
			if partial != tc.partial || failures != tc.failures || rows != wantRows || bad != wantBad || groups != 0 {
				t.Fatalf("persisted partial=%v failures=%d RAFTZ rows=%d bad=%d groups=%d", partial, failures, rows, bad, groups)
			}
			provider := &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true}
			response, err := provider.ShellStatus(ctx, &v1.GetShellStatusRequest{Range: &v1.TimeRange{Start: timestamppb.New(snap.StartedAt.Add(-time.Second)), End: timestamppb.New(snap.FinishedAt.Add(time.Second))}})
			if err != nil {
				t.Fatal(err)
			}
			if response.GetFreshness().GetEpoch() != epoch || response.GetFreshness().GetPartial() != tc.partial {
				t.Fatalf("shell freshness: %v", response.GetFreshness())
			}
			metrics, err := api.NewExposition("shop")
			if err != nil {
				t.Fatal(err)
			}
			if err := provider.WriteMetrics(ctx, metrics); err != nil {
				t.Fatal(err)
			}
			var text bytes.Buffer
			if _, err := metrics.WriteTo(&text); err != nil {
				t.Fatal(err)
			}
			value := 0
			if tc.partial {
				value = 1
			}
			if !strings.Contains(text.String(), fmt.Sprintf("nats_lens_last_epoch_partial{system=\"shop\"} %d\n", value)) {
				t.Fatalf("last partial metric absent: %s", text.String())
			}
			t.Logf("persisted partial=%v failures=%d RAFTZ:APP rows=%d non-ok=%d APP raft rows=%d", partial, failures, rows, bad, groups)
		})
	}
}

func monitoringError(id, name string) []byte {
	return fmt.Appendf(nil, `{"server":{"id":%q,"name":%q},"error":{"code":500,"description":"fixture unavailable"}}`, id, name)
}

func captureMonitoring(t *testing.T, ctx context.Context, nc *nats.Conn, e collect.Endpoint, ids []string) map[string][]byte {
	t.Helper()
	var opts any
	switch e {
	case collect.Jsz:
		opts = server.JSzOptions{Accounts: true, Streams: true, Consumer: true, Config: true, RaftGroups: true}
	case collect.Connz:
		opts = server.ConnzOptions{Username: true, Limit: 100}
	case collect.AccountStatz:
		opts = server.AccountStatzOptions{IncludeUnused: true}
	case collect.Healthz:
		opts = server.HealthzOptions{Details: true}
	case collect.AccountRaftz("APP"):
		opts = server.RaftzOptions{AccountFilter: "APP"}
	}
	body, err := json.Marshal(opts)
	if err != nil {
		t.Fatal(err)
	}
	inbox := nc.NewInbox()
	sub, err := nc.SubscribeSync(inbox)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe()
	if err := nc.PublishRequest(e.Subject(), inbox, body); err != nil {
		t.Fatal(err)
	}
	replies := map[string][]byte{}
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for len(replies) < len(ids) {
		msg, err := sub.NextMsgWithContext(requestCtx)
		if err != nil {
			t.Fatalf("capture %s: %v (%d replies)", e, err, len(replies))
		}
		var r struct {
			Server server.ServerInfo `json:"server"`
		}
		if err := json.Unmarshal(msg.Data, &r); err != nil {
			t.Fatal(err)
		}
		replies[r.Server.ID] = msg.Data
	}
	return replies
}

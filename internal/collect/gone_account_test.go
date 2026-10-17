package collect_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/testcluster"
)

// jszFake answers STATSZ always and JSZ while answerJSZ is set, reporting
// a stream in account APP when member is true and no account otherwise.
type jszFake struct {
	name, id  string
	member    bool
	answerJSZ atomic.Bool
}

func (f *jszFake) start(t *testing.T, nc *nats.Conn) {
	t.Helper()
	info := server.ServerInfo{Name: f.name, ID: f.id}
	statsz, _ := json.Marshal(server.ServerStatsMsg{Server: info})
	data := &server.JSInfo{ID: f.id}
	if f.member {
		data.AccountDetails = []*server.AccountDetail{{Name: "APP", Id: "APP",
			Streams: []server.StreamDetail{{Name: "catalog-changes"}}}}
	}
	jsz, _ := json.Marshal(map[string]any{"server": info, "data": data})
	subs := map[string][]byte{"$SYS.REQ.SERVER.PING.STATSZ": statsz, "$SYS.REQ.SERVER.PING.JSZ": jsz}
	for subj, body := range subs {
		_, err := nc.Subscribe(subj, func(m *nats.Msg) {
			if subj == "$SYS.REQ.SERVER.PING.JSZ" && !f.answerJSZ.Load() {
				return
			}
			_ = m.Respond(body)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	_ = nc.Flush()
}

// An account is gone only when every server answered JSZ without it. Here
// the member misses JSZ in the second epoch while a non-member answers: the
// account must still be asked about.
func TestAccountNotGoneWhileAMemberMissesJSZ(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	ctx := context.Background()
	c := testcluster.Start(t, 1)
	nc := c.Connect(0, testcluster.SysUser, testcluster.SysPass)
	member := &jszFake{name: "member", id: "IDMEMBER", member: true}
	other := &jszFake{name: "other", id: "IDOTHER"}
	member.answerJSZ.Store(true)
	other.answerJSZ.Store(true)
	member.start(t, nc)
	other.start(t, nc)
	col := newCol(nc, "nats-0", "member", "other")

	snap := col.Collect(ctx)
	if _, ok := snap.Servers[c.Servers[0].ID()].Status[collect.AccountRaftz("APP")]; !ok {
		t.Fatal("setup: APP RAFTZ not asked in the first epoch")
	}

	member.answerJSZ.Store(false)
	snap = col.Collect(ctx)
	if st := snap.Servers["IDMEMBER"].Status[collect.Jsz]; st.State != collect.StateMissing {
		t.Fatalf("setup: member JSZ = %+v, want missing", st)
	}
	if _, ok := snap.Servers[c.Servers[0].ID()].Status[collect.AccountRaftz("APP")]; !ok {
		t.Error("APP dropped as gone although its member did not answer JSZ")
	}

	// The next epoch still asks for it in the detail pass.
	member.answerJSZ.Store(true)
	snap = col.Collect(ctx)
	if _, ok := snap.Servers[c.Servers[0].ID()].Status[collect.AccountRaftz("APP")]; !ok {
		t.Error("APP not asked after the member answered again")
	}
}

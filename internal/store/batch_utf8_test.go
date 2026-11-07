package store

import (
	"testing"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// TestBatchInvalidUTF8 writes leaf nodes, routes and connections whose
// strings hold invalid UTF-8. The JSON batch stores each invalid byte as
// U+FFFD, as json.Marshal writes it, and the epoch is written; one INSERT
// per row could not bind such a string and failed the epoch.
func TestBatchInvalidUTF8(t *testing.T) {
	s := openMem(t)
	v := &collect.ServerView{
		Info:   server.ServerInfo{ID: "NUTF8"},
		Leafz:  &server.Leafz{Leafs: []*server.LeafInfo{{ID: 1, Name: "bad\xff\xfename", Account: "a\xc3"}}},
		Routez: &server.Routez{Routes: []*server.RouteInfo{{Rid: 1, RemoteName: "nats-\xff1"}}},
		Connz:  &server.Connz{Conns: []*server.ConnInfo{{Cid: 1, Name: "app\xff"}}},
	}
	writeWith(t, s, 1, v, func(w *writer, v *collect.ServerView) {
		w.writeLeafnodes(v)
		w.writeRoutes(v)
		w.writeConnections(v)
	})
	for _, c := range []struct{ q, want string }{
		{`SELECT name FROM leafnodes`, "bad��name"},
		{`SELECT account FROM leafnodes`, "a�"},
		{`SELECT remote_name FROM routes`, "nats-�1"},
		{`SELECT name FROM connections`, "app�"},
	} {
		var got string
		if err := s.DB().QueryRow(c.q).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("%s = %q, want %q", c.q, got, c.want)
		}
	}
}

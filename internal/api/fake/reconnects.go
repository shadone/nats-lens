package fake

import (
	"fmt"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// restartReconnects are the clients of each restarted server leaving
// when its pod was killed, at kill (a scenario time per server name),
// and connecting to its new process as the Connections page lists them:
// connection i at the process's start plus i times 7 seconds, named and
// addressed as there.
func restartReconnects(s scen, kills map[string]time.Time) []*v1.Event {
	var out []*v1.Event
	for _, sp := range s.sc.servers {
		kill, ok := kills[sp.name]
		if !ok {
			continue
		}
		start := s.started(sp)
		for i := range sp.conns {
			name := s.sc.clients[int(i)%len(s.sc.clients)]
			host := fmt.Sprintf("10.20.%d.%d", 1+i%4, 10+i)
			out = append(out,
				&v1.Event{Time: ts(kill.Add(time.Second + time.Duration(i)*20*time.Millisecond)), Source: srcNATS, Kind: "client.disconnect",
					Type: "CLIENT.DISCONNECT", Server: sp.name, Account: Account, ClientName: name, ClientHost: host, Reason: "Server Shutdown",
					Summary: fmt.Sprintf("%s disconnected from %s: Server Shutdown", name, sp.name)},
				&v1.Event{Time: ts(start.Add(time.Duration(i) * 7 * time.Second)), Source: srcNATS, Kind: "client.connect", Type: "CLIENT.CONNECT",
					Server: sp.name, Account: Account, ClientName: name, ClientHost: host,
					Summary: fmt.Sprintf("%s connected to %s from %s", name, sp.name, host)})
		}
	}
	return out
}

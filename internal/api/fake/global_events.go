package fake

import (
	"fmt"
	"strings"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// globalRoutineEvents are example-global's routine events over the last
// day, and the retries of the pod that cannot be scheduled: a poller's
// short connections, sampled acks, account connection counts, the
// collector's own reconnects during the pool-us upgrade and a CLI through
// a port-forward.
func globalRoutineEvents(s scen) []*v1.Event {
	var out []*v1.Event
	add := func(e *v1.Event) { out = append(out, e) }
	end := s.at(14, 29, 0)
	eu := func(k int) string { return fmt.Sprintf("nats-eu-%d", k%9) }
	// inventory-service: every 4.8 minutes a summary of 2 to 6, at most 15
	// in any 10 minutes, below client-005's threshold.
	for k := range 300 {
		add(&v1.Event{Time: ts(end.Add(-time.Duration(k) * 288 * time.Second)), Source: srcNATS, Kind: "client.churn", Type: "CLIENT.CHURN",
			Server: eu(k), Account: Account, ClientName: "inventory-service", ClientHost: fmt.Sprintf("10.30.0.%d", 60+k%4),
			Reason:  "Client Closed",
			Summary: fmt.Sprintf("inventory-service connected and disconnected %d times in 1 min (each closed by the client within 10 s)", 2+k%5)})
	}
	for k := range 48 {
		add(&v1.Event{Time: ts(end.Add(-time.Duration(k)*30*time.Minute - 40*time.Second)), Source: srcNATS, Kind: "consumer.ack_sampled",
			Type: "CONSUMER.ACK_SAMPLED", Server: "nats-us-3", Account: Account, Stream: "shipping-updates", Consumer: "label-printer",
			Summary: fmt.Sprintf("label-printer on shipping-updates acked after %d ms (1 delivery)", 120+k%5*40)})
		add(&v1.Event{Time: ts(end.Add(-time.Duration(k)*30*time.Minute - 15*time.Second)), Source: srcNATS, Kind: "account.connections",
			Type: "ACCOUNT.CONNECTIONS", Server: eu(k), Account: Account,
			Summary: fmt.Sprintf("account APP: %d connections, 0 leaf nodes", 1410+k%13)})
	}
	// The collector's connection to the us cluster moves as its servers
	// restart.
	for _, at := range []time.Time{s.at(8, 44, 7), s.at(8, 56, 22), s.at(9, 4, 52), s.at(9, 12, 2)} {
		for i, kind := range []string{"client.disconnect", "client.connect"} {
			add(&v1.Event{Time: ts(at.Add(time.Duration(i) * 2 * time.Second)), Source: srcNATS, Kind: kind, Type: strings.ToUpper(kind),
				Server: "nats-us-0", Account: "SYS", ClientName: "natslens-" + GlobalSystem, ClientHost: "10.30.9.40",
				Summary: "natslens-" + GlobalSystem + " " + strings.TrimPrefix(kind, "client.") + " from 10.30.9.40"})
		}
	}
	// Someone watches the upgrade with the CLI through a port-forward.
	for k := range 12 {
		at := s.at(8, 50, 0).Add(time.Duration(k) * 4 * time.Minute)
		for i, kind := range []string{"client.connect", "client.disconnect"} {
			add(&v1.Event{Time: ts(at.Add(time.Duration(i) * 5 * time.Second)), Source: srcNATS, Kind: kind, Type: strings.ToUpper(kind),
				Server: "nats-us-0", Account: "SYS", ClientName: "NATS CLI Version 0.2.2", ClientHost: "127.0.0.1",
				Summary: "NATS CLI Version 0.2.2 " + strings.TrimPrefix(kind, "client.") + " from 127.0.0.1"})
		}
	}
	// The scheduler retries nats-us-4 four more times after 09:12:20.
	for _, at := range []time.Time{s.at(9, 13, 30), s.at(9, 14, 45), s.at(9, 16, 10), s.at(9, 18, 0)} {
		add(&v1.Event{Time: ts(at), Source: srcK8s, Kind: "k8s.pod.failed_scheduling", Type: "FailedScheduling", Server: "nats-us-4",
			Summary: "pod nats-us-4: 0/4 nodes are available: 4 Insufficient memory"})
	}
	return out
}

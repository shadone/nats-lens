package store

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// TestNullEntriesOtherEndpoints writes one epoch per endpoint whose
// reply has a null entry in a list or map of pointers, and requires the
// epoch written without a panic or an error.
func TestNullEntriesOtherEndpoints(t *testing.T) {
	for _, c := range []struct {
		name string
		e    collect.Endpoint
		data string
	}{
		{"routez null route", collect.Routez, `{"server_id":"NR0","routes":[null,{"rid":1,"remote_name":"nats-1"}]}`},
		{"leafz null leaf", collect.Leafz, `{"server_id":"NR0","leafs":[null,{"name":"leaf-1"}]}`},
		{"connz null conn", collect.Connz, `{"server_id":"NR0","connections":[null,{"cid":1,"account":"APP"}]}`},
		{"gatewayz null inbound", collect.Gatewayz, `{"server_id":"NR0","outbound_gateways":{"east":null},"inbound_gateways":{"east":[null]}}`},
		{"accstatz null account", collect.AccountStatz, `{"server_id":"NR0","account_statz":[null]}`},
		{"jsz null meta replica", collect.Jsz, `{"server_id":"NR0","meta_cluster":{"name":"c","replicas":[null]}}`},
		{"raftz null group", collect.Raftz, `{"APP":{"S-x":null}}`},
		{"healthz null errors", collect.Healthz, `{"status":"ok","errors":[null]}`},
		{"subsz null sub", collect.Subsz, `{"server_id":"NR0","subscriptions_list":[null]}`},
		{"jsz nested nulls", collect.Jsz, `{"server_id":"NR0","meta_cluster":{"name":"c","replicas":null},"account_details":[{"name":"APP","id":"APP","api":null,"stream_detail":[{"name":"s","created":"2026-10-16T23:00:00Z","config":null,"cluster":null,"mirror":{"name":"m","error":null},"sources":null,"state":null,"consumer_detail":[{"stream_name":"s","name":"c","config":null,"cluster":{"name":"c","leader":"nats-0","replicas":null},"delivered":null,"ack_floor":null}]},{"name":"t","config":{"name":"t","sources":null,"mirror":null,"placement":null,"subjects":null},"cluster":{"replicas":[null]},"consumer_detail":[{"stream_name":"t","name":"d","config":{"backoff":null,"filter_subjects":null}}]}]}]}`},
		{"statsz nulls", collect.Statsz, `{"routes":[null],"gateways":[null]}`},
		{"varz nulls", collect.Varz, `{"server_id":"NR0","name":"nats-0","cluster":null,"gateway":null,"leaf":null,"jetstream":null}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := openMem(t)
			snap := collect.NewSnapshot(t0)
			mustApply(t, snap, collect.Varz, envelope(t, "nats-0", "NR0", server.Varz{ID: "NR0", Name: "nats-0"}))
			mustApply(t, snap, c.e, envelope(t, "nats-0", "NR0", json.RawMessage(c.data)))
			snap.FinishedAt = t0
			err := func() (err error) {
				defer func() {
					if p := recover(); p != nil {
						err = fmt.Errorf("panic: %v", p)
					}
				}()
				_, err = s.WriteSnapshot(context.Background(), snap)
				return err
			}()
			if err != nil {
				t.Error(err)
			}
		})
	}
}

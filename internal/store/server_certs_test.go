package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/collect"
)

// A VARZ reply of a server with TLS on the client and leaf node listeners;
// the other listeners have no TLS and omit the field.
const varzCertsReply = `{"server":{"name":"nats-0","id":"NCERTSERVER0","ver":"2.15.0"},
"data":{"server_id":"NCERTSERVER0","server_name":"nats-0","version":"2.15.0",
"tls_cert_not_after":"2026-11-05T12:00:00Z",
"cluster":{"name":"example-dev","cluster_port":6222},
"gateway":{},
"leaf":{"port":7422,"tls_cert_not_after":"2027-01-31T00:00:00Z"},
"websocket":{},"mqtt":{}}}`

// The listener certificates' expiry from VARZ is stored on server_stats;
// a listener without TLS is NULL.
func TestVarzCertsStored(t *testing.T) {
	s := openMem(t)
	snap := collect.NewSnapshot(t0)
	mustApply(t, snap, collect.Varz, []byte(varzCertsReply))
	if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	var client, cluster, gateway, leaf, ws, mqtt sql.NullTime
	if err := s.DB().QueryRow(`SELECT tls_cert_not_after, cluster_tls_cert_not_after, gateway_tls_cert_not_after,
    leafnode_tls_cert_not_after, websocket_tls_cert_not_after, mqtt_tls_cert_not_after FROM server_stats`).
		Scan(&client, &cluster, &gateway, &leaf, &ws, &mqtt); err != nil {
		t.Fatal(err)
	}
	if !client.Time.Equal(time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)) || !leaf.Time.Equal(time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("client %v, leaf %v", client, leaf)
	}
	if cluster.Valid || gateway.Valid || ws.Valid || mqtt.Valid {
		t.Errorf("listeners without TLS: cluster %v gateway %v websocket %v mqtt %v, want NULL", cluster, gateway, ws, mqtt)
	}
}

// Without VARZ the certificates are not known: NULL.
func TestVarzCertsNullWithoutVarz(t *testing.T) {
	s := openMem(t)
	snap := collect.NewSnapshot(t0)
	mustApply(t, snap, collect.Statsz, []byte(`{"server":{"name":"nats-0","id":"NCERTSERVER0","ver":"2.15.0"},"statsz":{"mem":1}}`))
	if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM server_stats WHERE tls_cert_not_after IS NULL
  AND cluster_tls_cert_not_after IS NULL AND gateway_tls_cert_not_after IS NULL AND leafnode_tls_cert_not_after IS NULL
  AND websocket_tls_cert_not_after IS NULL AND mqtt_tls_cert_not_after IS NULL`); n != 1 {
		t.Fatalf("rows with NULL certificates = %d, want 1", n)
	}
}

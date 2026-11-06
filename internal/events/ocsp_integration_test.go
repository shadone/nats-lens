package events_test

import (
	"context"
	"encoding/json"
	"log/slog"

	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"github.com/shadone/nats-lens/internal/events"
	"github.com/shadone/nats-lens/internal/store"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"golang.org/x/crypto/ocsp"
)

// A real TLS handshake against pinned nats-server, with a local CA and OCSP
// responder. No upstream fixture keys or external certificate calls are used.
func TestOCSPRealTLSScopedCapture(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a TLS NATS server and local OCSP responder")
	}
	now := time.Now().UTC()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "example.com test CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, SubjectKeyId: []byte{1, 2, 3, 4}}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	responder := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		var err error
		if r.Method == http.MethodPost {
			body, err = io.ReadAll(r.Body)
		} else {
			path, e := url.PathUnescape(strings.TrimPrefix(r.URL.EscapedPath(), "/"))
			err = e
			if err == nil {
				body, err = base64.StdEncoding.DecodeString(path)
			}
		}
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		req, err := ocsp.ParseRequest(body)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		status := ocsp.Good
		if req.SerialNumber.Int64() == 4 {
			status = ocsp.Revoked
		}
		response, err := ocsp.CreateResponse(ca, ca, ocsp.Response{Status: status, SerialNumber: req.SerialNumber,
			ThisUpdate: now.Add(-time.Minute), NextUpdate: now.Add(time.Hour), RevokedAt: now.Add(-time.Hour)}, caKey)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/ocsp-response")
		_, _ = w.Write(response)
	}))
	defer responder.Close()
	dir := t.TempDir()
	writePEM := func(name, typ string, der []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	caPath := writePEM("ca.pem", "CERTIFICATE", caDER)
	issue := func(serial int64, name string) (tls.Certificate, string, string) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		template := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: name},
			NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
			IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, OCSPServer: []string{responder.URL}}
		der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		keyDER, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		certPath, keyPath := writePEM(name+".pem", "CERTIFICATE", der), writePEM(name+".key", "EC PRIVATE KEY", keyDER)
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			t.Fatal(err)
		}
		return cert, certPath, keyPath
	}
	_, certPath, keyPath := issue(2, "server.example.com")
	good, _, _ := issue(3, "capture.example.com")
	bad, _, _ := issue(4, "revoked.example.com")
	config := fmt.Sprintf(`server_name: nats-0
listen: 127.0.0.1:-1
tls { cert_file: %q, key_file: %q, ca_file: %q, verify: true, ocsp_peer: true }
accounts {
 SYS { users: [ { user: sys, password: sys-test, permissions: {
  publish: { allow: ["$SYS.REQ.SERVER.PING.>", "$SYS.REQ.ACCOUNT.PING.STATZ"] },
  subscribe: { allow: ["$SYS.ACCOUNT.>", "$SYS.SERVER.>", "$JS.EVENT.ADVISORY.>", "_INBOX.>"] }
 } } ] }
 APP { users: [ { user: app, password: app-test } ] }
}
system_account: SYS
`, certPath, keyPath, caPath)
	configPath := filepath.Join(dir, "nats.conf")
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	opts, err := server.ProcessConfigFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	opts.NoLog, opts.NoSigs = true, true
	opts.OCSPCacheConfig = server.NewOCSPResponseCacheConfig()
	opts.OCSPCacheConfig.LocalStore = filepath.Join(dir, "ocsp-cache")
	s, err := server.NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	defer s.Shutdown()
	if !s.ReadyForConnections(5 * time.Second) {
		t.Fatal("TLS server not ready")
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	tlsConfig := func(cert tls.Certificate) *tls.Config {
		return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: []tls.Certificate{cert}}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	capture := events.New(events.Options{Writer: st, FlushInterval: 20 * time.Millisecond, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := capture.Run(ctx); err != nil {
			t.Errorf("capture: %v", err)
		}
	}()
	stop := func() { cancel(); <-done }
	defer stop()
	nc, err := nats.Connect(s.ClientURL(), nats.UserInfo("sys", "sys-test"), nats.Secure(tlsConfig(good)))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	if err := capture.SubscribeSys(nc); err != nil {
		t.Fatal(err)
	}
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}
	// A valid peer connects and emits no rejection; a revoked peer cannot connect.
	app, err := nats.Connect(s.ClientURL(), nats.UserInfo("app", "app-test"), nats.Secure(tlsConfig(good)))
	if err != nil {
		t.Fatal(err)
	}
	app.Close()
	refused, err := nats.Connect(s.ClientURL(), nats.UserInfo("app", "app-test"), nats.Secure(tlsConfig(bad)), nats.Timeout(time.Second), nats.NoReconnect())
	if err == nil {
		refused.Close()
		t.Fatal("revoked peer connected")
	}
	// Inspect the full stored record, not only the in-memory decoded raw field.
	waitRow(t, st.DB(), "OCSP rejection", `SELECT raw::VARCHAR FROM events WHERE kind='server.ocsp_peer_rejected'`)
	stop()
	rows, err := st.DB().Query(`SELECT to_json(e)::VARCHAR FROM events e WHERE kind IN ('server.ocsp_peer_rejected','collector.subscription_error')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	rejects := 0
	for rows.Next() {
		var record string
		if err := rows.Scan(&record); err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"revoked.example.com", "example.com test CA", base64.StdEncoding.EncodeToString(bad.Certificate[0]), `\"raw\"`, `\"subject\"`, `\"issuer\"`} {
			if strings.Contains(record, secret) {
				t.Fatalf("certificate identity in stored record: %s", record)
			}
		}
		var ev struct {
			Kind       string          `json:"kind"`
			ServerName string          `json:"server_name"`
			Reason     string          `json:"reason"`
			Raw        json.RawMessage `json:"raw"`
		}
		if err := json.Unmarshal([]byte(record), &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Kind != "server.ocsp_peer_rejected" || ev.ServerName != "nats-0" || ev.Reason != "client not OCSP valid" {
			t.Fatalf("stored rejection: %s", record)
		}
		var raw struct {
			Peer map[string]string `json:"peer"`
		}
		if err := json.Unmarshal(ev.Raw, &raw); err != nil {
			t.Fatal(err)
		}
		if len(raw.Peer) != 1 || raw.Peer["fingerprint"] == "" {
			t.Fatalf("stored peer: %s", ev.Raw)
		}
		rejects++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if rejects != 1 {
		t.Fatalf("got %d final rejections, want 1", rejects)
	}
}

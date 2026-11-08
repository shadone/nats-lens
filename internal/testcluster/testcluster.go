// Package testcluster starts an in-process JetStream cluster for tests: a
// SYS system account and an APP JetStream account, with routes between
// all servers on random local ports and data in a temporary directory.
package testcluster

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// Users of the test cluster. These are test-only credentials.
const (
	SysUser = "sys"
	SysPass = "sys-test"
	AppUser = "app"
	AppPass = "app-test"
	// ScopedSysUser and ScopedAppUser hold only the permissions the
	// README asks for, so tests show that they are enough.
	ScopedSysUser = "sys-scoped"
	ScopedSysPass = "sys-scoped-test"
	ScopedAppUser = "app-scoped"
	ScopedAppPass = "app-scoped-test"
	ClusterName   = "lens-test"
	serverPrefix  = "nats-"
)

// Cluster is a running set of servers.
type Cluster struct {
	t       testing.TB
	Servers []*server.Server
	Names   []string
	confs   []string
}

const confTemplate = `
server_name: %s
listen: 127.0.0.1:%d
jetstream { store_dir: %q, max_memory_store: 64MB, max_file_store: 512MB }
accounts {
  SYS { users: [ { user: %s, password: %s },
    { user: sys-scoped, password: sys-scoped-test, permissions: {
      publish: { allow: ["$SYS.REQ.SERVER.PING.>", "$SYS.REQ.ACCOUNT.PING.STATZ"] },
      subscribe: { allow: ["$SYS.ACCOUNT.>", "$SYS.SERVER.>", "$JS.EVENT.ADVISORY.>", "_INBOX.>"] } } } ] }
  APP { jetstream: enabled, users: [ { user: %s, password: %s },
    { user: app-scoped, password: app-scoped-test, permissions: {
      publish: { allow: ["$SYS.REQ.USER.INFO", "$SRV.INFO", "$SRV.STATS"] },
      subscribe: { allow: ["$JS.EVENT.>", "$SYS.ACCOUNT.CLIENT.AUTH.ERR", "_INBOX.>"] } } } ] }
}
system_account: SYS
%s`

const clusterTemplate = `cluster {
  name: %s
  listen: 127.0.0.1:%d
  routes: [ %s ]
}
`

func freePort(t testing.TB) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// Start starts n servers named nats-0 .. nats-(n-1). With n >= 2 they are
// routed into a JetStream cluster and Start waits until it has a meta
// leader that knows every server; with n = 1 the server runs JetStream
// without a cluster. Servers are shut down when the test ends.
// Short mode skips callers before starting any NATS server.
func Start(t testing.TB, n int) *Cluster {
	t.Helper()
	return start(t, n, 0)
}

// FreePort returns a TCP port free on 127.0.0.1 now.
func FreePort(t testing.TB) int { return freePort(t) }

// StartOnPort starts one server, nats-0, listening for clients on port,
// for a test whose client was configured before the server existed.
func StartOnPort(t testing.TB, port int) *Cluster {
	t.Helper()
	return start(t, 1, port)
}

func start(t testing.TB, n, port int) *Cluster {
	t.Helper()
	if testing.Short() {
		t.Skip("local NATS integration: run without -short")
	}
	if n < 1 {
		t.Fatalf("testcluster: need at least one server, got %d", n)
	}
	dir := t.TempDir()
	clientPorts := make([]int, n)
	routePorts := make([]int, n)
	for i := range n {
		clientPorts[i], routePorts[i] = freePort(t), freePort(t)
	}
	if port != 0 {
		clientPorts[0] = port
	}
	var routes []string
	for _, p := range routePorts {
		routes = append(routes, fmt.Sprintf("nats-route://127.0.0.1:%d", p))
	}
	c := &Cluster{t: t}
	for i := range n {
		name := fmt.Sprintf("%s%d", serverPrefix, i)
		clusterBlock := ""
		if n > 1 {
			clusterBlock = fmt.Sprintf(clusterTemplate, ClusterName, routePorts[i], strings.Join(routes, ", "))
		}
		conf := fmt.Sprintf(confTemplate, name, clientPorts[i], filepath.Join(dir, name),
			SysUser, SysPass, AppUser, AppPass, clusterBlock)
		path := filepath.Join(dir, name+".conf")
		if err := os.WriteFile(path, []byte(conf), 0o600); err != nil {
			t.Fatal(err)
		}
		c.confs = append(c.confs, path)
		c.Servers = append(c.Servers, c.boot(path, name))
		c.Names = append(c.Names, name)
	}
	t.Cleanup(c.Shutdown)
	if n > 1 {
		c.waitForMetaLeader()
	}
	return c
}

// boot starts a server from a config file and waits until it accepts clients.
// options reads a server's config file with the settings tests override.
func (c *Cluster) options(path, name string) *server.Options {
	c.t.Helper()
	opts, err := server.ProcessConfigFile(path)
	if err != nil {
		c.t.Fatalf("config %s: %v", name, err)
	}
	opts.NoLog, opts.NoSigs = true, true
	// The config file rejects lame duck durations under 30s; tests set
	// them directly so that LameDuck finishes quickly.
	opts.LameDuckDuration, opts.LameDuckGracePeriod = 2*time.Second, 500*time.Millisecond
	return opts
}

// SetSysPassword changes the password of the SYS user on every server and
// reloads the configuration, as a credentials rotation does: connections
// that authenticated with the old password are closed by the servers and
// fail to log in again.
func (c *Cluster) SetSysPassword(from, to string) {
	c.t.Helper()
	old := fmt.Sprintf("{ user: %s, password: %s }", SysUser, from)
	for i, path := range c.confs {
		b, err := os.ReadFile(path)
		if err != nil {
			c.t.Fatal(err)
		}
		if !strings.Contains(string(b), old) {
			c.t.Fatalf("%s: SYS password is not %q", path, from)
		}
		conf := strings.Replace(string(b), old, fmt.Sprintf("{ user: %s, password: %s }", SysUser, to), 1)
		if err := os.WriteFile(path, []byte(conf), 0o600); err != nil {
			c.t.Fatal(err)
		}
		if err := c.Servers[i].ReloadOptions(c.options(path, c.Names[i])); err != nil {
			c.t.Fatalf("reload %s: %v", c.Names[i], err)
		}
	}
}

func (c *Cluster) boot(path, name string) *server.Server {
	c.t.Helper()
	opts := c.options(path, name)
	s, err := server.NewServer(opts)
	if err != nil {
		c.t.Fatalf("server %s: %v", name, err)
	}
	go s.Start()
	if !s.ReadyForConnections(10 * time.Second) {
		c.t.Fatalf("server %s not ready", name)
	}
	return s
}

// Restart starts server i again after Stop, with the same configuration and
// data. The new process has a new server ID. It waits until the meta
// leader knows every server again.
func (c *Cluster) Restart(i int) {
	c.t.Helper()
	c.Servers[i] = c.boot(c.confs[i], c.Names[i])
	if len(c.Servers) > 1 {
		c.waitForMetaLeader()
	}
}

// MetaLeaderTimeout bounds the wait for a JetStream meta leader with every
// peer. It is generous because a full parallel go test run can starve the
// in-process cluster for a long time.
var MetaLeaderTimeout = 60 * time.Second

func (c *Cluster) waitForMetaLeader() {
	c.t.Helper()
	deadline := time.Now().Add(MetaLeaderTimeout)
	for time.Now().Before(deadline) {
		for _, s := range c.Servers {
			if s.JetStreamIsLeader() && len(s.JetStreamClusterPeers()) == len(c.Servers) {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	c.t.Fatal("no JetStream meta leader with every peer")
}

// URL returns the client URL of server i.
func (c *Cluster) URL(i int) string { return c.Servers[i].ClientURL() }

// Connect connects to server i as the given user.
func (c *Cluster) Connect(i int, user, pass string) *nats.Conn {
	c.t.Helper()
	nc, err := nats.Connect(c.URL(i), nats.UserInfo(user, pass), nats.MaxReconnects(0))
	if err != nil {
		c.t.Fatal(err)
	}
	c.t.Cleanup(nc.Close)
	return nc
}

// Stop shuts down server i and waits for it to exit.
func (c *Cluster) Stop(i int) {
	c.Servers[i].Shutdown()
	c.Servers[i].WaitForShutdown()
}

// LameDuck puts server i into lame duck mode, which ends in a shutdown, and
// waits for it to exit.
func (c *Cluster) LameDuck(i int) {
	c.Servers[i].LameDuckShutdown()
	c.Servers[i].WaitForShutdown()
}

// Shutdown stops every server.
func (c *Cluster) Shutdown() {
	for _, s := range c.Servers {
		s.Shutdown()
		s.WaitForShutdown()
	}
}

// RestartAsync starts server i again without waiting for the meta leader.
func (c *Cluster) RestartAsync(i int) {
	c.t.Helper()
	c.Servers[i] = c.boot(c.confs[i], c.Names[i])
}

// WaitMeta waits for the meta leader.
func (c *Cluster) WaitMeta() { c.waitForMetaLeader() }

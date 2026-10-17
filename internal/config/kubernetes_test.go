package config

import (
	"strings"
	"testing"
	"time"
)

func TestKubernetesDefaultsAndFile(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	cfg, err := Parse("natslens", nil)
	if err != nil {
		t.Fatal(err)
	}
	k := cfg.Kubernetes
	if k.Enabled || k.NATSSelector != DefaultNATSSelector || k.ServerName != "{pod}" || k.NodeEventsNamespace != "default" ||
		!k.Nodes || k.GapAfter.D() != 30*time.Second {
		t.Fatalf("defaults: %+v", k)
	}
	p := writeFile(t, `
kubernetes:
  enabled: true
  namespaces: [example]
  nats_selector: app=nats
  server_name: "{namespace}-{pod}"
  nodes: false
  gap_after: 1m
`)
	cfg, err = Parse("natslens", []string{"-config", p})
	if err != nil {
		t.Fatal(err)
	}
	k = cfg.Kubernetes
	if !k.Enabled || len(k.Namespaces) != 1 || k.Namespaces[0] != "example" || k.NATSSelector != "app=nats" ||
		k.ServerName != "{namespace}-{pod}" || k.Nodes || k.PoolNodes || k.GapAfter.D() != time.Minute || k.NodeEventsNamespace != "default" {
		t.Fatalf("file: %+v", k)
	}
}

func TestKubernetesValidate(t *testing.T) {
	cases := map[string]struct {
		edit func(*Config)
		want string
	}{
		"needs events":   {func(c *Config) { c.Events.Enabled = false }, "needs events.enabled"},
		"selector":       {func(c *Config) { c.Kubernetes.NATSSelector = " " }, "nats_selector"},
		"server name":    {func(c *Config) { c.Kubernetes.ServerName = "nats" }, "must contain {pod}"},
		"gap":            {func(c *Config) { c.Kubernetes.GapAfter = 0 }, "gap_after"},
		"namespace":      {func(c *Config) { c.Kubernetes.Namespaces = []string{"example", ""} }, "namespaces[1]"},
		"node namespace": {func(c *Config) { c.Kubernetes.NodeEventsNamespace = "" }, "node_events_namespace"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := Default()
			c.Kubernetes.Enabled = true
			tc.edit(&c)
			err := c.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
	c := Default()
	c.Events.Enabled = false
	c.Kubernetes.ServerName = ""
	if err := c.Validate(); err != nil {
		t.Fatalf("a disabled watcher is not validated: %v", err)
	}
}

func TestKubernetesClientPodsPattern(t *testing.T) {
	t.Setenv(PasswordEnv, "")
	p := writeFile(t, "kubernetes:\n  client_pods: '^batch-job-'\n")
	cfg, err := Parse("natslens", []string{"-config", p})
	if err != nil {
		t.Fatal(err)
	}
	if re := cfg.Kubernetes.ClientPodsPattern(); re == nil || !re.MatchString("batch-job-1") {
		t.Errorf("pattern %v", re)
	}
	p = writeFile(t, "kubernetes:\n  client_pods: '('\n")
	if _, err := Parse("natslens", []string{"-config", p}); err == nil {
		t.Error("invalid pattern accepted")
	}
}

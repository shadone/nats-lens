package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/shadone/nats-lens/internal/config"
)

// checkSystems is the check before any system starts: each system's data
// directory belongs to it and every file it needs exists. One error names
// every broken system, so that a start shows all of them at once. A
// failure after this check (a connection, a file that later breaks)
// isolates its system instead of stopping the others.
func checkSystems(systems []config.System) error {
	var errs []error
	for _, sys := range systems {
		if err := checkMarker(sys); err != nil {
			errs = append(errs, err)
		}
		if err := missingFiles(sys); err != nil {
			errs = append(errs, fmt.Errorf("system %s: %w", sys.ID, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("not starting: %w", errors.Join(errs...))
	}
	return nil
}

// missingFiles lists the files the system's configuration names that
// cannot be read: credentials, TLS files, the kubeconfig and the
// inventory file.
func missingFiles(sys config.System) error {
	var errs []error
	need := func(key, path string) {
		if path == "" {
			return
		}
		if f, err := os.Open(path); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
		} else {
			_ = f.Close()
		}
	}
	natsFiles := func(prefix string, n config.NATS) {
		need(prefix+".creds", n.Creds)
		need(prefix+".tls.ca", n.TLS.CA)
		need(prefix+".tls.cert", n.TLS.Cert)
		need(prefix+".tls.key", n.TLS.Key)
	}
	natsFiles("nats", sys.NATS)
	for _, a := range sys.Accounts {
		// The CA a connection inherits from nats is reported there.
		c := a.Connection(sys.NATS)
		if a.TLS.CA == "" {
			c.TLS.CA = ""
		}
		natsFiles("accounts["+a.Name+"]", c)
	}
	if sys.Kubernetes.Enabled {
		need("kubernetes.kubeconfig", sys.Kubernetes.Kubeconfig)
	}
	need("inventory_file", sys.InventoryFile)
	return errors.Join(errs...)
}

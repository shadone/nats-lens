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
// withEvents is whether the command starts the Kubernetes watcher
// (natslens run with events.enabled): only then is the kubeconfig read.
// The files of the accounts under accounts are not checked: an account
// that cannot connect is dialled again while the system collects.
func checkSystems(systems []config.System, withEvents bool) error {
	var errs []error
	for _, sys := range systems {
		if err := checkMarker(sys); err != nil {
			errs = append(errs, err)
		}
		if err := missingFiles(sys, withEvents); err != nil {
			errs = append(errs, fmt.Errorf("system %s: %w", sys.ID, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("not starting: %w", errors.Join(errs...))
	}
	return nil
}

// missingFiles lists the files the system's configuration names that
// cannot be read and the command reads: the nats credentials and TLS
// files and the inventory file always, the kubeconfig withEvents.
func missingFiles(sys config.System, withEvents bool) error {
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
	need("inventory_file", sys.InventoryFile)
	if withEvents && sys.Kubernetes.Enabled {
		need("kubernetes.kubeconfig", sys.Kubernetes.Kubeconfig)
	}
	return errors.Join(errs...)
}

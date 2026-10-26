package main

import (
	"errors"
	"fmt"
	"log/slog"
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
// A missing file of an entry under accounts refuses no start, as an
// account that cannot connect is dialled again while the system collects;
// each is logged as a warning naming the system, the entry and the file.
func checkSystems(systems []config.System, withEvents bool, log *slog.Logger) error {
	var errs []error
	for _, sys := range systems {
		for _, m := range missingAccountFiles(sys) {
			log.Warn("account file missing: the account is not connected and is dialled again until the file can be read",
				"system", sys.ID, "account", m.account, "file", m.key, "path", m.path, "err", m.err)
		}
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
	for _, f := range natsFiles(sys.NATS) {
		need("nats."+f.key, f.path)
	}
	need("inventory_file", sys.InventoryFile)
	if withEvents && sys.Kubernetes.Enabled {
		need("kubernetes.kubeconfig", sys.Kubernetes.Kubeconfig)
	}
	return errors.Join(errs...)
}

// natsFile is a file a connection's settings name.
type natsFile struct{ key, path string }

// natsFiles lists the files of c that are set, keyed as in the
// configuration.
func natsFiles(c config.NATS) []natsFile {
	var out []natsFile
	for _, f := range []natsFile{{"creds", c.Creds}, {"tls.ca", c.TLS.CA}, {"tls.cert", c.TLS.Cert}, {"tls.key", c.TLS.Key}} {
		if f.path != "" {
			out = append(out, f)
		}
	}
	return out
}

// missingAccountFile is a file of an entry under accounts that cannot be
// opened.
type missingAccountFile struct {
	account, key, path string
	err                error
}

// missingAccountFiles lists the files of the system's accounts entries
// that cannot be opened. A CA the entry inherits from nats is checked
// there, not here.
func missingAccountFiles(sys config.System) []missingAccountFile {
	var out []missingAccountFile
	for _, a := range sys.Accounts {
		c := a.Connection(sys.NATS)
		if a.TLS.CA == "" {
			c.TLS.CA = ""
		}
		for _, f := range natsFiles(c) {
			fh, err := os.Open(f.path)
			if err != nil {
				out = append(out, missingAccountFile{account: a.Name, key: "accounts[" + a.Name + "]." + f.key, path: f.path, err: err})
				continue
			}
			_ = fh.Close()
		}
	}
	return out
}

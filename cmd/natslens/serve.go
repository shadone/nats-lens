package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/fake"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/iap"
	"github.com/shadone/nats-lens/internal/webui"
)

// apiOptions configure the HTTP side of the API.
type apiOptions struct {
	listen       string
	allowedHosts []string
	iapAudience  string
	// ready reports whether /readyz answers ok; nil means always.
	ready func() bool
	// metricsTimeout bounds each system's part of a /metrics scrape; zero
	// means api.DefaultMetricsTimeout.
	metricsTimeout time.Duration
}

// startAPI listens and serves the API and UI for the systems until ctx is
// done. Listening happens before it returns, so a taken port fails the
// command at start. The returned wait blocks until the server stopped and every
// call returned, so the caller may then close the store.
func startAPI(ctx context.Context, o apiOptions, systems api.Systems, log *slog.Logger) (wait func(), err error) {
	if err := systems.Validate(); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", o.listen)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", o.listen, err)
	}
	h := &api.Handler{Systems: systems, Log: log, Version: version, MetricsTimeout: o.metricsTimeout}
	if o.iapAudience != "" {
		h.IAP = &iap.Verifier{Audience: o.iapAudience, Keys: iap.NewJWKS()}
	}
	hosts := o.allowedHosts
	if len(hosts) == 0 {
		hosts = api.DefaultAllowedHosts(o.listen)
	}
	log.Info("api settings", "allowed_hosts", hosts, "iap", o.iapAudience != "")
	// /readyz, like /healthz, answers every host: the kubelet probes the
	// pod IP.
	mux := api.WithReady(api.AllowHosts(hosts, api.NewMux(h, webui.FS()), api.HostExemptPaths(o.listen)...), o.ready)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := api.ServeListener(ctx, ln, mux, log); err != nil {
			log.Error("api server stopped", "err", err)
		}
	}()
	return func() { <-done }, nil
}

// runDemo serves the fake providers without NATS or a database: the
// reference scenario as example-prod, the default system, then
// example-global and example-staging.
func runDemo(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("natslens demo", flag.ContinueOnError)
	listen := fs.String("listen", api.DefaultListen, "address for the API and UI")
	allowed := fs.String("allowed-host", "", "comma-separated Host values the API accepts (default: loopback names when listening on loopback, else any)")
	fixed := fs.Bool("fixed", false, "keep the scenario at its own time (2026-10-17 14:30 UTC) instead of shifting it to now")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	log.Info("starting", "command", "demo", "version", version, "live", !*fixed)
	wait, err := startAPI(ctx, apiOptions{listen: *listen, allowedHosts: config.SplitList(*allowed)},
		fake.Demo(!*fixed), log)
	if err != nil {
		return err
	}
	wait()
	return nil
}

// singleSystem serves p as the one system "default", until the
// configuration lists systems.
func singleSystem(p api.Provider) api.Systems {
	return api.One(api.DefaultSystemID, api.DefaultSystemID, p)
}

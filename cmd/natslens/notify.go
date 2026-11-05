package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/notify"
)

// checkNotifications refuses channel filters that name an unknown check
// or category, and for natslens run, which is the command that notifies,
// warns of each secret that cannot be read now and of a missing ui_url.
// An unreadable secret refuses no start: the file is read again at each
// delivery attempt.
func checkNotifications(cfg config.Config, set *checks.Set, cmd string, log *slog.Logger) error {
	if err := notify.CheckFilters(cfg.Notifications, set); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if cmd != "run" || len(cfg.Notifications.Channels) == 0 {
		return nil
	}
	for _, ch := range cfg.Notifications.Channels {
		if err := notify.SecretProblem(ch); err != nil {
			log.Warn("notification channel cannot be sent to until its URL or token can be read", "channel", ch.Name, "err", err)
		}
	}
	if cfg.Notifications.UIURL == "" {
		log.Warn("notifications.ui_url is not set: notifications carry no link to the finding")
	}
	return nil
}

// runNotifyCmd serves natslens notify test, printing one line per channel
// to out.
func runNotifyCmd(ctx context.Context, args []string, out io.Writer) error {
	const usage = "usage: natslens notify test -config FILE [-channel NAME] [-system ID]"
	if len(args) == 0 || args[0] != "test" {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet("natslens notify test", flag.ContinueOnError)
	path := fs.String("config", "", "configuration file whose notifications.channels are tested")
	only := fs.String("channel", "", "test only the channel with this name (default: every channel)")
	system := fs.String("system", "", "system the test message names (default: the channel's first system)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New(usage)
	}
	if *path == "" {
		return errors.New("-config is required: the channels are in the configuration file")
	}
	cfg, err := config.LoadFile(*path, config.Default())
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	set, err := loadChecks(cfg)
	if err != nil {
		return err
	}
	if err := checkNotifications(cfg, set, "notify", slog.Default()); err != nil {
		return err
	}
	channels := cfg.Notifications.Channels
	if len(channels) == 0 {
		return errors.New("the configuration has no notifications.channels")
	}
	if *only != "" {
		ch, ok := cfg.Notifications.Channel(*only)
		if !ok {
			return fmt.Errorf("unknown channel %q; configured channels: %s", *only, strings.Join(cfg.Notifications.ChannelNames(), ", "))
		}
		channels = []config.Channel{ch}
	}
	var fixed *config.System
	if *system != "" {
		sys, err := pickSystem(cfg, *system)
		if err != nil {
			return err
		}
		fixed = &sys
	}
	failed := 0
	for _, ch := range channels {
		sys := testSystem(cfg, ch, fixed)
		b := notify.TestBatch(sys.ID, sys.Name, version, cfg.Notifications.UIURL, time.Now())
		r := notify.NewSender(ch, version).Send(ctx, b)
		if r.Outcome != notify.Delivered {
			failed++
		}
		name := ch.Name
		if h := hostOf(r.Host); h != "" {
			name += " (" + h + ")"
		}
		if _, err := fmt.Fprintf(out, "%s: %s\n", name, r); err != nil {
			return err
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d channels failed", failed, len(channels))
	}
	return nil
}

// testSystem is the system a channel's test message names: fixed when
// set, else the first system the channel receives.
func testSystem(cfg config.Config, ch config.Channel, fixed *config.System) config.System {
	if fixed != nil {
		return *fixed
	}
	systems := cfg.ResolvedSystems()
	if len(ch.Systems) > 0 {
		for _, s := range systems {
			if s.ID == ch.Systems[0] {
				return s
			}
		}
	}
	return systems[0]
}

// hostOf is the host of a "scheme://host" value.
func hostOf(schemeHost string) string {
	_, h, ok := strings.Cut(schemeHost, "://")
	if !ok {
		return ""
	}
	return h
}

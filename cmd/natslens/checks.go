package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/inventory"
	"github.com/shadone/nats-lens/internal/store"
)

// loadChecks reads the check files with the configured overrides. Every
// system shares the result; each engine sets its system's tooling names.
func loadChecks(cfg config.Config) (*checks.Set, error) {
	set, err := checks.Load(cfg.Checks)
	if err != nil {
		return nil, fmt.Errorf("checks: %w", err)
	}
	if cfg.Checks.RemediationFile != "" {
		slog.Info("remediation file loaded", "file", cfg.Checks.RemediationFile, "checks_extended", set.RemediationExtended())
	}
	return set, nil
}

// loadInventory replaces the inventory table with the inventory file, or
// empties it without one, so that a removed file stops the inventory checks.
func loadInventory(ctx context.Context, cfg config.Config, st *store.Store, log *slog.Logger) error {
	var rows []store.InventoryRow
	if cfg.InventoryFile != "" {
		var err error
		if rows, err = inventory.Load(cfg.InventoryFile); err != nil {
			return err
		}
	}
	if err := st.ReplaceInventory(ctx, rows, cfg.InventoryFile, time.Now()); err != nil {
		return fmt.Errorf("store inventory: %w", err)
	}
	if cfg.InventoryFile != "" {
		log.Info("inventory loaded", "file", cfg.InventoryFile, "assets", len(rows))
	}
	return nil
}

// runChecksCmd serves natslens checks list and natslens checks run.
func runChecksCmd(ctx context.Context, args []string, out io.Writer) error {
	const usage = "usage: natslens checks list [-config FILE] | natslens checks run [-config FILE] [-system ID] [-data-dir DIR] [-epoch N]"
	if len(args) == 0 {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet("natslens checks "+args[0], flag.ContinueOnError)
	db := oneDatabaseFlags(fs)
	epoch := fs.Int64("epoch", 0, "epoch to check (default: the newest)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "list":
		cfg := config.Default()
		if *db.path != "" {
			var err error
			if cfg, err = config.LoadFile(*db.path, cfg); err != nil {
				return err
			}
		}
		set, err := loadChecks(cfg)
		if err != nil {
			return err
		}
		return listChecks(set, out)
	case "run":
		cfg, sys, dir, err := db.resolve()
		if err != nil {
			return err
		}
		set, err := loadChecks(cfg)
		if err != nil {
			return err
		}
		lim := storeLimits(cfg.ForSystem(sys), runtimeProcs(), dir)
		return runChecksOnce(ctx, set, sys.ToolingNames(), filepath.Join(dir, store.FileName), *epoch, out, cfg.Interval.D(), lim)
	}
	return errors.New(usage)
}

func listChecks(set *checks.Set, out io.Writer) error {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CODE\tNAME\tENTITY\tCATEGORY\tSEVERITY\tKIND\tLOOKBACK\tTHRESHOLDS\tSTATE")
	for _, c := range set.Checks() {
		var th []string
		for _, t := range c.Thresholds {
			v, _ := set.Value(c.Code, t.Name)
			th = append(th, t.Name+"="+v.String())
		}
		lookback := "-"
		if lb := set.Lookback(c.Code); lb > 0 {
			lookback = checks.DurationText(lb)
		}
		severity := c.Severity
		if c.SeverityRule != "" {
			severity += "+"
		}
		state := "on"
		if set.Disabled(c.Code) {
			state = "disabled"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", c.Code, c.Name, c.Entity, c.Category, severity, c.Kind,
			lookback, orDash(strings.Join(th, " ")), state)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out, "\nA severity with + changes per finding; the header of checks/<code>.sql says how.")
	return err
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// checksDBReady is called once runChecksOnce's database has its limits;
// a variable so that tests can read them.
var checksDBReady = func(context.Context, *sql.DB) {}

// runChecksOnce evaluates every check for one epoch of a database no
// collector holds, read-only, and prints what fails. It stores nothing.
// tooling are the nats-lens connection names of the database's system.
// The database gets the system's memory limit and spill bound, lim.
func runChecksOnce(ctx context.Context, set *checks.Set, tooling []string, path string, epoch int64, out io.Writer, interval time.Duration, lim limits) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	db, err := sql.Open("duckdb", path+"?access_mode=read_only")
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("open %s (is a collector running on it?): %w", path, err)
	}
	if err := store.CheckSchema(ctx, db); err != nil {
		return err
	}
	// Both settings are the database's, so one statement covers every
	// connection of the pool.
	if _, err := db.ExecContext(ctx, fmt.Sprintf("SET memory_limit = '%dB'", lim.memory)); err != nil {
		return fmt.Errorf("set memory_limit: %w", err)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf("SET max_temp_directory_size = '%dB'", lim.spill)); err != nil {
		return fmt.Errorf("set max_temp_directory_size: %w", err)
	}
	checksDBReady(ctx, db)
	if epoch == 0 {
		if err := db.QueryRowContext(ctx, `SELECT coalesce(max(epoch), 0) FROM epochs`).Scan(&epoch); err != nil {
			return err
		}
		if epoch == 0 {
			return errors.New("the database holds no epoch")
		}
	}
	runs, err := (&checks.Engine{Set: set, Interval: interval, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), ToolingNames: tooling}).Evaluate(ctx, db, epoch)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CODE\tSTATUS\tSEVERITY\tENTITY\tDETAIL")
	failing, errs := 0, 0
	for _, r := range runs {
		switch r.Status {
		case store.RunError:
			errs++
			fmt.Fprintf(tw, "%s\terror\t-\t-\t%s\n", r.Code, oneLine(r.Error))
		case store.RunDisabled:
			fmt.Fprintf(tw, "%s\tdisabled\t-\t-\t-\n", r.Code)
		default:
			rows := append([]store.FindingRow(nil), r.Rows...)
			sort.Slice(rows, func(i, j int) bool { return rows[i].EntityKey < rows[j].EntityKey })
			for _, row := range rows {
				failing++
				fmt.Fprintf(tw, "%s\tfiring\t%s\t%s\t%s\n", r.Code, row.Severity, orDash(row.EntityKey), oneLine(row.Detail))
			}
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "(epoch %d: %d checks, %d failing entities, %d checks with errors; nothing stored)\n",
		epoch, len(runs), failing, errs)
	return err
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

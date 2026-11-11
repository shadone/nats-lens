// Command natslens-perf builds synthetic stores and measures the API and
// the store against them; docs/operations/performance-baseline.md records
// its results and how to run it.
//
//	natslens-perf generate -profile NAME -out FILE
//	natslens-perf load -profile NAME -db FILE [-scenarios LIST] [-json FILE]
//	natslens-perf explain -db FILE [-out DIR]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/pprof"
	"slices"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/perf"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "natslens-perf:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: natslens-perf generate|load|explain [flags]")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	profile := fs.String("profile", "smoke", "profile: "+strings.Join(profileNames(), ", "))
	out := fs.String("out", "", "generate: the store file to write; explain: the directory for the plans")
	db := fs.String("db", "", "the generated store file")
	scenarios := fs.String("scenarios", "", "load: comma-separated scenarios; empty runs all: "+strings.Join(perf.ScenarioNames(), ", "))
	duration := fs.Duration("duration", 60*time.Second, "load: how long each concurrent scenario runs")
	jsonOut := fs.String("json", "", "load: also write the results as JSON to this file")
	cpuProfile := fs.String("cpuprofile", "", "load: write a Go CPU profile of the run to this file; it does not show DuckDB's native work")
	budget := fs.Int("query-budget", -1, "load: the store's query budget; -1 derives it from the threads as natslens does, 0 leaves the API unbounded")
	threads := fs.Int("threads", 0, "load: DuckDB threads; 0 keeps DuckDB's default")
	var memLimit config.ByteSize
	fs.Func("memory-limit", "load: DuckDB memory_limit, such as 4GiB; unset keeps DuckDB's default", func(v string) error {
		return memLimit.UnmarshalText([]byte(v))
	})
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	p, ok := perf.Profiles[*profile]
	if !ok {
		return fmt.Errorf("unknown profile %q", *profile)
	}
	switch args[0] {
	case "generate":
		if *out == "" {
			return fmt.Errorf("generate needs -out")
		}
		res, err := perf.Generate(ctx, p, *out, log)
		if err != nil {
			return err
		}
		b, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(b))
		return nil
	case "load":
		if *db == "" {
			return fmt.Errorf("load needs -db")
		}
		if *cpuProfile != "" {
			f, err := os.Create(*cpuProfile)
			if err != nil {
				return err
			}
			defer f.Close()
			if err := pprof.StartCPUProfile(f); err != nil {
				return err
			}
			defer pprof.StopCPUProfile()
		}
		var names []string
		if *scenarios != "" {
			names = strings.Split(*scenarios, ",")
		}
		rep, err := perf.Load(ctx, perf.LoadOptions{Profile: p, Path: *db, Scenarios: names, Duration: *duration, Log: log,
			QueryBudget: *budget, Threads: *threads, MemoryLimit: int64(memLimit)})
		if err != nil {
			return err
		}
		fmt.Print(rep.Markdown())
		if *jsonOut != "" {
			b, _ := json.MarshalIndent(rep, "", "  ")
			return os.WriteFile(*jsonOut, b, 0o600)
		}
		return nil
	case "explain":
		if *db == "" || *out == "" {
			return fmt.Errorf("explain needs -db and -out")
		}
		return perf.Explain(ctx, *db, p, *out, log)
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func profileNames() []string {
	var out []string
	for n := range perf.Profiles {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

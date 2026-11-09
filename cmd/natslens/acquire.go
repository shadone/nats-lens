package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/shadone/nats-lens/internal/acquisition"
	"github.com/shadone/nats-lens/internal/dataset"
)

type acquisitionOptions struct {
	socket, directory string
	maxBytes          int64
}

func startAcquisition(ctx context.Context, o acquisitionOptions, rts []*systemRuntime) (func(), error) {
	if o.socket == "" && o.directory == "" {
		return func() {}, nil
	}
	var sources []acquisition.Source
	for _, rt := range rts {
		retention := int64(rt.sys.Retention.D())
		sources = append(sources, acquisition.Source{Store: rt.st, System: dataset.System{ID: rt.sys.ID, Name: rt.sys.Name, CollectionIntervalNS: int64(rt.cfg.Interval.D()), RetentionNS: &retention}})
	}
	return acquisition.Start(ctx, acquisition.Options{Socket: o.socket, Directory: o.directory, ApplicationVersion: version, Budget: dataset.Options{MaxBytes: o.maxBytes}}, sources)
}
func runAcquire(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("natslens acquire", flag.ContinueOnError)
	fs.SetOutput(out)
	socket := fs.String("socket", "", "explicitly enabled local administrative Unix socket")
	system := fs.String("system", "", "system id: acquisition exports all retained per-system data")
	fs.Usage = func() {
		fmt.Fprintln(out, "Usage: natslens acquire -socket PATH -system ID\nCreates a private server-local dataset; prints its path and manifest as JSON. No transfer or upload.\nRequires an explicitly enabled same-user local administrative socket.\nNative byte/time cancellation is cooperative; hard disk isolation requires a filesystem quota.\nThe source read transaction can retain source MVCC/WAL history until native work unwinds.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *socket == "" || *system == "" {
		return fmt.Errorf("acquire requires -socket and -system and no positional arguments")
	}
	result, err := acquisition.Request(ctx, *socket, *system)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(result)
}

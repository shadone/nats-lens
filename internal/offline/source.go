// Package offline composes captured data with the authoritative read-only API.
// Only trusted, closed, schema-compatible datasets are accepted. Integrity
// checks do not establish authenticity or sandbox a malicious native database.
package offline

import (
	"context"
	"database/sql"
	"errors"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/dataset"
	"github.com/shadone/nats-lens/internal/investigation"
)

func Open(ctx context.Context, selection investigation.OfflineSelection) (*investigation.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if (selection.DB == "") == (selection.Bundle == "") {
		return nil, errors.New("choose exactly one offline source")
	}
	var db *sql.DB
	var bounds dataset.Boundaries
	var provenance any
	var system dataset.System
	var version string
	var fingerprint string
	var err error
	if selection.Bundle != "" {
		d, e := dataset.Open(ctx, selection.Bundle)
		if e != nil {
			return nil, sourceError(ctx, e)
		}
		db, bounds, system, version = d.DB, d.Manifest.Boundaries, d.Manifest.System, d.Manifest.ApplicationVersion
		fingerprint = d.Manifest.Data.SHA256
		provenance = map[string]any{"manifest": d.Manifest, "identity": "captured manifest", "limitations": limitations}
	} else {
		if selection.System == "" || selection.Interval <= 0 {
			return nil, errors.New("bare database requires explicit system and positive collection interval assertions")
		}
		db, err = dataset.OpenDatabase(ctx, selection.DB)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		fingerprint, err = dataset.FingerprintDatabase(ctx, selection.DB)
		if err != nil {
			db.Close()
			return nil, sourceError(ctx, err)
		}
		bounds, err = dataset.InspectDatabase(ctx, db)
		if err != nil {
			db.Close()
			return nil, sourceError(ctx, err)
		}
		system = dataset.System{ID: selection.System, CollectionIntervalNS: int64(selection.Interval)}
		provenance = map[string]any{"fingerprint": fingerprint, "identity": "operator-supplied system assertion; original configured identity is unavailable", "system": system, "boundaries": bounds, "captureStartedAt": nil, "exportedAt": nil, "limitations": limitations}
	}
	fail := func(err error) (*investigation.Session, error) { db.Close(); return nil, err }
	if bounds.DataReference == nil || bounds.DataReference.IsZero() {
		return fail(&investigation.Diagnostic{Code: "no_reference_clock", Message: "dataset has no completed collection reference clock"})
	}
	if bounds.LastCollectionStartedAt == nil || bounds.DataReference.Before(*bounds.LastCollectionStartedAt) {
		return fail(&investigation.Diagnostic{Code: "no_reference_clock", Message: "dataset collection reference must follow its half-open sample boundary"})
	}
	queryReference := *bounds.DataReference
	adjustment := "none"
	if queryReference.Equal(*bounds.LastCollectionStartedAt) {
		queryReference = queryReference.Add(time.Microsecond)
		adjustment = "exclusive_end_inclusion: one database-resolution tick; not an observed capture time"
	}
	if timestamppb.New(*bounds.DataReference).CheckValid() != nil || timestamppb.New(queryReference).CheckValid() != nil {
		return fail(&investigation.Diagnostic{Code: "no_reference_clock", Message: "dataset reference or derived exclusive end is outside the API timestamp range"})
	}
	provenance = map[string]any{"capture": provenance, "dataReference": bounds.DataReference, "queryReference": queryReference, "queryReferenceAdjustment": adjustment}
	p, err := storeprov.NewCaptured(ctx, db, time.Duration(system.CollectionIntervalNS), *bounds.DataReference, fingerprint)
	if err != nil {
		return fail(sourceError(ctx, err))
	}
	p.RangeClock = func() time.Time { return queryReference }
	if system.RetentionNS != nil {
		p.Retention = time.Duration(*system.RetentionNS)
		p.RetentionKnown = true
	}
	h := &api.Handler{Systems: api.One(system.ID, system.Name, p), Version: version}
	return &investigation.Session{Source: investigation.NewInProcess(api.NewInvestigationHandler(h)), Provenance: provenance, QueryReference: queryReference, Close: db.Close}, nil
}

var limitations = []string{
	"Only the selected captured system is present; this file does not describe a configured fleet",
	"Runtime collector/archiver status, current readiness, private remediation and check overrides are unavailable",
	"Absent side metadata cannot be reconstructed; retention-deleted rows and configured policy may be unknown",
	"Data reference is the newest completed collection finish; events may extend beyond it, as boundaries report",
	"Default tooling and Kubernetes interpretation settings were not captured; configuration-dependent classifications require external verification",
	"No remote UI serves this dataset; reuse context with the same offline source and retrieve typed evidence IDs",
}

func sourceError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	code := "source_unavailable"
	switch {
	case errors.Is(err, dataset.ErrVersion):
		code = "unsupported_source_version"
	case errors.Is(err, dataset.ErrIntegrity):
		code = "source_integrity"
	case errors.Is(err, dataset.ErrInvalid):
		code = "invalid_source"
	}
	return &investigation.Diagnostic{Code: code, Message: "offline source cannot be opened; use a trusted closed compatible dataset without WAL; conversion or repair must use a separate explicit copy"}
}

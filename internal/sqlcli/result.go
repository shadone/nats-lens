package sqlcli

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	duckdb "github.com/duckdb/duckdb-go/v2"
	"github.com/duckdb/duckdb-go/v2/mapping"
)

const DefaultMaxRows = 1000
const DefaultMaxBytes = 1 << 20
const DefaultTimeout = 45 * time.Second

// Options are presentation budgets, not a sandbox or SQL execution-cost bound.
type Options struct {
	Format            string
	MaxRows, MaxBytes int
	Timeout           time.Duration
}

func DefaultOptions() Options {
	return Options{"table", DefaultMaxRows, DefaultMaxBytes, DefaultTimeout}
}

// Diagnostic intentionally omits backend text, which can contain SQL, data or paths.
// The version/code/message/nextStep shape matches the investigation convention.
type Diagnostic struct {
	Version     int `json:"version"`
	ErrorDetail struct {
		Code     string `json:"code"`
		Message  string `json:"message"`
		NextStep string `json:"nextStep"`
	} `json:"error"`
}

func (d *Diagnostic) Error() string { b, _ := json.Marshal(d); return string(b) }
func diagnostic(code, message, next string) error {
	d := &Diagnostic{Version: 1}
	d.ErrorDetail.Code = code
	d.ErrorDetail.Message = message
	d.ErrorDetail.NextStep = next
	return d
}
func Invalid(message string) error {
	return diagnostic("invalid_argument", message, "Check natslens sql -help.")
}

// ConfigurationError deliberately omits config values, filenames and system IDs.
func ConfigurationError() error {
	return diagnostic("invalid_argument", "Cannot resolve database configuration or system selection", "Check the local configuration file and -system selection, or use -db with an existing offline snapshot.")
}

func queryError(ctx context.Context) error {
	if ctx.Err() != nil {
		return diagnostic("cancelled", "SQL cancelled or time budget expired", "Narrow the trusted query or adjust -timeout within 45s.")
	}
	return diagnostic("query_failed", "SQL execution or value conversion failed", "Check trusted SQL and column types; no result was published.")
}

type column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type result struct {
	Version int `json:"version"`
	Source  struct {
		Kind     string `json:"kind"`
		Path     string `json:"path"`
		ReadOnly bool   `json:"readOnly"`
		Snapshot string `json:"snapshot"`
	} `json:"source"`
	Columns   []column          `json:"columns"`
	Rows      []json.RawMessage `json:"rows"`
	Truncated bool              `json:"truncated"`
	Limits    struct {
		Rows        int    `json:"rows"`
		OutputBytes int    `json:"outputBytes"`
		Timeout     string `json:"timeout"`
	} `json:"limits"`
}

// SourcePath resolves local filenames without interpreting them as DSNs. The pinned
// driver splits raw DSNs at '?' and does not URL-decode its filename (ASVS V1.2.2).
func SourcePath(path string) (string, error) {
	p, err := filepath.Abs(path)
	if err != nil {
		return "", Invalid("Invalid database path")
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return "", diagnostic("source_unavailable", "Database file unavailable", "Select an existing consistent offline database with -db.")
	}
	if strings.ContainsAny(p, "?#%") {
		return "", Invalid("Database paths containing ?, # or % are unsupported by the pinned DuckDB driver; use a safely named offline copy")
	}
	st, err := os.Stat(p)
	if err != nil || !st.Mode().IsRegular() {
		return "", Invalid("Database source must be a regular file")
	}
	return p, nil
}

// Execute buffers a complete document. Query errors, cancellation and byte-budget
// errors return no data. The original SQL is passed unchanged, including statements
// before the final result; trusted SQL can have external side effects (ASVS V1.2.4).
func Execute(ctx context.Context, path, query string, o Options) ([]byte, error) {
	if (o.Format != "table" && o.Format != "json") || o.MaxRows < 1 || o.MaxRows > 100000 || o.MaxBytes < 1024 || o.MaxBytes > 16<<20 || o.Timeout <= 0 || o.Timeout > DefaultTimeout {
		return nil, Invalid("Budgets require rows 1–100000, output bytes 1024–16777216, timeout greater than zero and at most 45s, format table or json")
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	if ctx.Err() != nil {
		return nil, queryError(ctx)
	}
	path, err := SourcePath(path)
	if err != nil {
		return nil, err
	}
	connector, err := duckdb.NewConnector(path+"?access_mode=read_only", nil)
	if err != nil {
		return nil, diagnostic("source_unavailable", "Cannot open database read-only (lock, version or file error)", "Select a validated consistent snapshot; do not copy a live database file alone or repair the source.")
	}
	defer connector.Close()
	conn, err := connector.Connect(ctx)
	if err != nil {
		return nil, queryError(ctx)
	}
	defer conn.Close()
	// Preparing through the pinned native API exposes nested logical aliases that
	// database/sql's type-name strings omit. PrepareContext executes preceding
	// statements exactly once, as the driver's QueryContext does; no SQL rewriting.
	prepared, err := conn.(driver.ConnPrepareContext).PrepareContext(ctx, query)
	if err != nil {
		return nil, queryError(ctx)
	}
	defer prepared.Close()
	stmt := prepared.(*duckdb.Stmt)
	n, err := stmt.ColumnCount()
	if err != nil {
		return nil, queryError(ctx)
	}
	var logicalTypes []duckdb.TypeInfo
	if o.Format == "json" {
		logicalTypes = make([]duckdb.TypeInfo, n)
		for i := 0; i < n; i++ {
			info, e := stmt.ColumnTypeInfo(i)
			if e != nil {
				return nil, queryError(ctx)
			}
			if hasClockType(info) {
				return nil, diagnostic("unsupported_type", "TIME, TIMETZ and TIME_NS values cannot be returned losslessly by the pinned driver", "Explicitly cast TIME, TIMETZ and TIME_NS values, including nested members, to VARCHAR in the trusted SQL.")
			}
			logicalTypes[i] = info
			if hasJSONAlias(info) {
				return nil, diagnostic("unsupported_type", "DuckDB JSON values cannot be returned losslessly by the pinned driver", "Explicitly cast JSON values, including nested members, to VARCHAR or BLOB in the trusted SQL.")
			}
		}
	}
	rows, err := stmt.QueryContext(ctx, nil)
	if err != nil {
		return nil, queryError(ctx)
	}
	defer rows.Close()
	names := rows.Columns()
	types := make([]column, len(names))
	meta := rows.(driver.RowsColumnTypeDatabaseTypeName)
	for i, name := range names {
		if !utf8.ValidString(name) {
			return nil, queryError(ctx)
		}
		types[i] = column{name, meta.ColumnTypeDatabaseTypeName(i)}
	}
	r := result{Version: 1, Columns: make([]column, len(types)), Rows: make([]json.RawMessage, 0)}
	r.Source.Kind = "database"
	r.Source.Path = path
	r.Source.ReadOnly = true
	r.Source.Snapshot = "unverified; caller-selected offline file"
	r.Limits.Rows = o.MaxRows
	r.Limits.OutputBytes = o.MaxBytes
	r.Limits.Timeout = o.Timeout.String()
	for i, c := range types {
		r.Columns[i] = c
	}
	var table limitedBuffer
	table.limit = o.MaxBytes
	tw := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	if o.Format == "table" {
		names := make([]string, len(types))
		for i, c := range types {
			names[i] = c.Name
		}
		if _, err = fmt.Fprintln(tw, strings.Join(names, "\t")); err != nil {
			return nil, outputLimit()
		}
	}
	vals := make([]driver.Value, len(types))
	count, used := 0, 0
	for {
		err = rows.Next(vals)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, queryError(ctx)
		}
		if ctx.Err() != nil {
			return nil, queryError(ctx)
		}
		if count == o.MaxRows {
			r.Truncated = true
			break
		}
		if o.Format == "json" {
			normalized := make([]any, len(vals))
			for i, v := range vals {
				if hasNonfiniteTimestampNS(logicalTypes[i], v) {
					return nil, temporalDiagnostic()
				}
				normalized[i], err = jsonValue(v)
				if err != nil {
					if errors.Is(err, errTemporalRepresentation) {
						return nil, temporalDiagnostic()
					}
					return nil, queryError(ctx)
				}
			}
			b, e := json.Marshal(normalized)
			if e != nil {
				return nil, queryError(ctx)
			}
			used += len(b) + 1
			if used > o.MaxBytes {
				return nil, outputLimit()
			}
			r.Rows = append(r.Rows, b)
		} else {
			cells := make([]string, len(vals))
			for i, v := range vals {
				cells[i] = format(v)
			}
			if _, err = fmt.Fprintln(tw, strings.Join(cells, "\t")); err != nil {
				return nil, outputLimit()
			}
		}
		count++
	}
	if ctx.Err() != nil {
		return nil, queryError(ctx)
	}
	var b []byte
	if o.Format == "json" {
		b, err = json.Marshal(r)
		if err != nil {
			return nil, queryError(ctx)
		}
		b = append(b, '\n')
	} else {
		if err = tw.Flush(); err != nil {
			return nil, outputLimit()
		}
		if _, err = fmt.Fprintf(&table, "(%d rows)\n", count); err != nil {
			return nil, outputLimit()
		}
		if r.Truncated {
			if _, err = fmt.Fprintln(&table, "(truncated: row budget reached)"); err != nil {
				return nil, outputLimit()
			}
		}
		b = table.Bytes()
	}
	if len(b) > o.MaxBytes {
		return nil, outputLimit()
	}
	if ctx.Err() != nil {
		return nil, queryError(ctx)
	}
	return b, nil
}
func outputLimit() error {
	return diagnostic("output_limit", "SQL output exceeds byte budget; no result published", "Narrow the trusted query or raise -max-output-bytes within 16777216.")
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, outputLimit()
	}
	return b.Buffer.Write(p)
}

func jsonValue(v any) (any, error) {
	switch x := v.(type) {
	case nil, bool:
		return x, nil
	case string:
		if !utf8.ValidString(x) {
			return nil, errors.New("invalid UTF-8")
		}
		return x, nil
	case []byte:
		return base64.StdEncoding.EncodeToString(x), nil
	case *big.Int:
		return x.String(), nil
	case duckdb.Decimal:
		return x.String(), nil
	case time.Time:
		// MarshalText validates RFC 3339 year bounds; Format alone does not.
		b, err := x.UTC().MarshalText()
		if err != nil {
			return nil, errTemporalRepresentation
		}
		return string(b), nil
	case duckdb.Interval:
		return map[string]string{"months": strconv.FormatInt(int64(x.Months), 10), "days": strconv.FormatInt(int64(x.Days), 10), "micros": strconv.FormatInt(x.Micros, 10)}, nil
	case duckdb.UUID:
		return x.String(), nil
	case duckdb.Bit:
		return x.String(), nil
	case duckdb.Union:
		y, e := jsonValue(x.Value)
		return map[string]any{"tag": x.Tag, "value": y}, e
	case duckdb.OrderedMap:
		keys, values := x.Keys(), x.Values()
		pairs := make([][2]any, len(keys))
		for i := range keys {
			k, e := jsonValue(keys[i])
			if e != nil {
				return nil, e
			}
			v, e := jsonValue(values[i])
			if e != nil {
				return nil, e
			}
			pairs[i] = [2]any{k, v}
		}
		return pairs, nil
	case []any:
		r := make([]any, len(x))
		for i, v := range x {
			y, e := jsonValue(v)
			if e != nil {
				return nil, e
			}
			r[i] = y
		}
		return r, nil
	case map[string]any:
		r := make(map[string]any, len(x))
		for k, v := range x {
			if !utf8.ValidString(k) {
				return nil, errors.New("invalid UTF-8")
			}
			y, e := jsonValue(v)
			if e != nil {
				return nil, e
			}
			r[k] = y
		}
		return r, nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(rv.Uint(), 10), nil
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return strconv.FormatFloat(f, 'g', -1, rv.Type().Bits()), nil
		}
		return v, nil
	}
	return nil, fmt.Errorf("unsupported SQL value %T", v)
}

// Publish never replaces an existing path. A same-directory temporary file and
// atomic link publish prevent incomplete successful files or source replacement
// through symlinks/hardlinks (ASVS V5.3.2). No SQL-side effects are rolled back.
func Publish(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".natslens-sql-*")
	if err != nil {
		return publishError()
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return publishError()
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return publishError()
	}
	if err = f.Close(); err != nil {
		return publishError()
	}
	if err = os.Link(name, path); err != nil {
		return publishError()
	}
	return nil
}
func publishError() error {
	return diagnostic("output_failed", "Cannot publish output file; destination must not exist", "Choose a new file in an existing writable directory.")
}
func Write(out io.Writer, b []byte) error {
	n, err := out.Write(b)
	if err != nil || n != len(b) {
		return diagnostic("output_failed", "Output write failed; discard any received bytes", "Retry with -output and a new destination file.")
	}
	return nil
}

// The driver unmarshals JSON through float64, losing numeric precision and
// duplicate object keys. Its nested type-name strings hide JSON as VARCHAR.
// Reject aliases recursively rather than guessing from values or rewriting SQL.
func hasJSONAlias(info duckdb.TypeInfo) bool {
	return hasLogicalType(info, func(t duckdb.TypeInfo) bool { return t.Alias() == "JSON" })
}

// getTime loses 24:00 by taking only the clock fields of time.UnixMicro;
// getTimeTZ constructs a calendar date and UTC serialization drops the offset.
// TIME_NS has native type metadata but no vector conversion in this driver.
// Reject the declared families, even null/empty results, before scanning values.
// Inspecting an already converted midnight cannot establish its original value.
func hasClockType(info duckdb.TypeInfo) bool {
	return hasLogicalType(info, func(t duckdb.TypeInfo) bool {
		return t.InternalType() == duckdb.TYPE_TIME || t.InternalType() == duckdb.TYPE_TIME_TZ || t.InternalType() == mapping.TypeTimeNS
	})
}

// Share one metadata traversal so every unsupported family covers the same
// nested containers, including map keys and unselected union members.
func hasLogicalType(info duckdb.TypeInfo, match func(duckdb.TypeInfo) bool) bool {
	if match(info) {
		return true
	}
	switch d := info.Details().(type) {
	case *duckdb.ListDetails:
		return hasLogicalType(d.Child, match)
	case *duckdb.ArrayDetails:
		return hasLogicalType(d.Child, match)
	case *duckdb.MapDetails:
		return hasLogicalType(d.Key, match) || hasLogicalType(d.Value, match)
	case *duckdb.StructDetails:
		for _, e := range d.Entries {
			if hasLogicalType(e.Info(), match) {
				return true
			}
		}
	case *duckdb.UnionDetails:
		for _, m := range d.Members {
			if hasLogicalType(m.Type, match) {
				return true
			}
		}
	}
	return false
}

var errTemporalRepresentation = errors.New("temporal value cannot be represented faithfully")

func temporalDiagnostic() error {
	return diagnostic("unsupported_value", "SQL temporal value cannot be represented faithfully as RFC 3339 JSON", "Explicitly cast temporal values, including nested members, to VARCHAR in the trusted SQL to preserve infinity or out-of-range values.")
}

// The pinned driver converts TIMESTAMP_NS ticks with time.Unix(0,ticks), even
// for infinity. Those dates fall inside RFC 3339's year range. Check their exact
// ticks with the pinned native finiteness API, using declared logical types;
// do not classify ordinary finite dates by resemblance to a sentinel date.
func hasNonfiniteTimestampNS(info duckdb.TypeInfo, v any) bool {
	if v == nil {
		return false
	}
	if info.InternalType() == duckdb.TYPE_TIMESTAMP_NS {
		t, ok := v.(time.Time)
		return ok && !mapping.IsFiniteTimestampNS(mapping.NewTimestampNS(t.UnixNano()))
	}
	switch d := info.Details().(type) {
	case *duckdb.ListDetails:
		for _, x := range v.([]any) {
			if hasNonfiniteTimestampNS(d.Child, x) {
				return true
			}
		}
	case *duckdb.ArrayDetails:
		for _, x := range v.([]any) {
			if hasNonfiniteTimestampNS(d.Child, x) {
				return true
			}
		}
	case *duckdb.MapDetails:
		m := v.(duckdb.OrderedMap)
		keys, values := m.Keys(), m.Values()
		for i, k := range keys {
			if hasNonfiniteTimestampNS(d.Key, k) || hasNonfiniteTimestampNS(d.Value, values[i]) {
				return true
			}
		}
	case *duckdb.StructDetails:
		m := v.(map[string]any)
		for _, e := range d.Entries {
			if hasNonfiniteTimestampNS(e.Info(), m[e.Name()]) {
				return true
			}
		}
	case *duckdb.UnionDetails:
		u := v.(duckdb.Union)
		for _, m := range d.Members {
			if m.Name == u.Tag {
				return hasNonfiniteTimestampNS(m.Type, u.Value)
			}
		}
	}
	return false
}

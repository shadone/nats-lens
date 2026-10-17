// Package sqlcli runs one SQL query against a natslens database file opened
// read-only and prints the result as an aligned table.
package sqlcli

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	_ "github.com/duckdb/duckdb-go/v2" // registers the "duckdb" driver
)

// Run opens path read-only, runs query and writes the rows to out. DuckDB
// lets only one process open a file while it is written, so Run fails while
// a collector has the file open; stop the collector first.
func Run(ctx context.Context, path, query string, out io.Writer) error {
	db, err := sql.Open("duckdb", path+"?access_mode=read_only")
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(cols, "\t"))
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	n := 0
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		cells := make([]string, len(vals))
		for i, v := range vals {
			cells[i] = format(v)
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
		n++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "(%d rows)\n", n)
	return err
}

func format(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case time.Time:
		return x.UTC().Format("2006-01-02 15:04:05.000")
	case []byte:
		return string(x)
	case string:
		return strings.ReplaceAll(x, "\t", " ")
	}
	return fmt.Sprint(v)
}

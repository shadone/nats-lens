// Package sqlcli runs one SQL query against a natslens database file opened
// read-only and prints the result as an aligned table.
package sqlcli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// Run preserves the default human table with bounded output. It opens only an
// existing database read-only; consistent acquisition remains caller-owned.
func Run(ctx context.Context, path, query string, out io.Writer) error {
	b, err := Execute(ctx, path, query, DefaultOptions())
	if err != nil {
		return err
	}
	return Write(out, b)
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

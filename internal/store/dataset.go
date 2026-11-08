package store

import (
	"context"
	"database/sql"
	"fmt"
	duckdb "github.com/duckdb/duckdb-go/v2"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// SnapshotCopy copies the source catalog and rows using one source read transaction.
// inspect reads capture provenance in that same transaction. path must be a new,
// caller-owned private destination, never the source database.
func (s *Store) SnapshotCopy(ctx context.Context, path string, inspect func(*sql.Tx) error) error {
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return fmt.Errorf("snapshot destination must not exist")
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	alias := fmt.Sprintf("dataset_%d", snapshotID.Add(1))
	if _, err = conn.ExecContext(ctx, "ATTACH "+sqlString(path)+" AS "+alias); err != nil {
		return err
	}
	defer func() {
		clean, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(clean, "DETACH "+alias)
	}()
	var source string
	if err = conn.QueryRowContext(ctx, "SELECT current_database()").Scan(&source); err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Pin the source snapshot before reading provenance or copying the catalog.
	var n int64
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM epochs").Scan(&n); err != nil {
		return err
	}
	if inspect != nil {
		if err = inspect(tx); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "COPY FROM DATABASE \""+strings.ReplaceAll(source, "\"", "\"\"")+"\" TO "+alias); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, "CHECKPOINT "+alias); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, "DETACH "+alias)
	return err
}

var snapshotID atomic.Uint64

// ReadOnlyDB opens a closed, trusted database without migration, checkpoint,
// recovery, or repair. It uses the same fatal-engine guard as the collector.
// The caller owns Close. External access and extension loading are disabled;
// this is defense in depth, not a sandbox for arbitrary untrusted databases.
func ReadOnlyDB(ctx context.Context, path string) (*sql.DB, error) {
	connector, err := duckdb.NewConnector(path+"?access_mode=read_only&enable_external_access=false&autoload_known_extensions=false&autoinstall_known_extensions=false", nil)
	if err != nil {
		return nil, SafeError("offline open", err)
	}
	db := sql.OpenDB(&guardedConnector{Connector: connector, g: newEngineGuard()})
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err = CheckSchemaIntegrity(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// CheckSchemaIntegrity verifies every migration checksum without filling missing
// values or accepting/rewriting legacy checksums. It never modifies the store.
func CheckSchemaIntegrity(ctx context.Context, db *sql.DB) error {
	all, err := loadMigrations()
	if err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx, "SELECT version,name,sha256 FROM migrations ORDER BY version")
	if err != nil {
		return err
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var v int
		var name, sum string
		if err = rows.Scan(&v, &name, &sum); err != nil {
			return err
		}
		if i >= len(all) || v != all[i].version || name != all[i].name || sum != all[i].sha256 {
			return fmt.Errorf("schema migration checksum or version mismatch")
		}
		i++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if i != len(all) {
		return fmt.Errorf("schema migration set incomplete")
	}
	return nil
}

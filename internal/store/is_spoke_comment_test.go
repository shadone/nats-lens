package store

import (
	"context"
	"strings"
	"testing"
)

// The comment on leafnodes.is_spoke says what nats-server sets: true on
// the server that opened the connection to a hub.
func TestIsSpokeComment(t *testing.T) {
	s := openMem(t)
	var comment string
	if err := s.DB().QueryRowContext(context.Background(), `SELECT comment FROM duckdb_columns()
WHERE table_name = 'leafnodes' AND column_name = 'is_spoke'`).Scan(&comment); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(comment, "reporting server is the spoke") {
		t.Errorf("is_spoke comment = %q", comment)
	}
}

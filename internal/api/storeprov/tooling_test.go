package storeprov

import (
	"database/sql"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
)

// The Go and SQL forms of the tooling rule agree on every name and
// address, with loopback on and off; a missing name or address is
// neither a tooling name nor a loopback address.
func TestToolingRuleGoAndSQLAgree(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	type conn struct {
		name, ip sql.NullString
	}
	str := func(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
	conns := []conn{
		{str("natslens-shop"), str("10.0.0.5")},
		{str("orders-api@pod-1"), str("10.0.0.6")},
		{str("orders-api@pod-1"), str("127.0.0.1")},
		{str("nats-cli"), str("::1")},
		{str(""), str("10.0.0.7")},
		{sql.NullString{}, str("127.0.0.1")},
		{str("orders-api"), sql.NullString{}},
		{sql.NullString{}, sql.NullString{}},
	}
	for _, loopback := range []bool{true, false} {
		rule := toolingRule{names: []string{"natslens-shop", "natslens-shop-events"}, loopback: loopback}
		cond, args := rule.sql("name", "ip")
		for _, c := range conns {
			var got bool
			if err := db.QueryRow(`SELECT `+cond+` FROM (SELECT ?::VARCHAR AS name, ?::VARCHAR AS ip)`,
				append(args, c.name, c.ip)...).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if want := rule.is(c.name.String, c.ip.String); got != want {
				t.Errorf("loopback %v, %+v: SQL %v, Go %v", loopback, c, got, want)
			}
		}
	}
}

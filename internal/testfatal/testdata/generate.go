//go:build ignore

// Generate the synthetic fixture with duckdb-go v2.10506.0, never the upgraded
// engine. Run create and replay as separate processes; see README.md.
package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/duckdb/duckdb-go/v2"
)

func main() {
	if len(os.Args) != 3 {
		panic("usage: generate create|replay NEW_DISPOSABLE_PATH")
	}
	db, err := sql.Open("duckdb", os.Args[2])
	must(err)
	db.SetMaxOpenConns(1)
	var version string
	must(db.QueryRow("SELECT version()").Scan(&version))
	fmt.Println(version)
	if version != "v1.5.6" {
		panic("fixture requires DuckDB v1.5.6")
	}
	switch os.Args[1] {
	case "create":
		for _, q := range []string{
			`CREATE TABLE fatal_fixture(epoch BIGINT, server_id VARCHAR, endpoint VARCHAR, state VARCHAR NOT NULL)`,
			`CREATE UNIQUE INDEX fatal_fixture_key ON fatal_fixture(epoch,server_id,endpoint)`,
			`INSERT INTO fatal_fixture SELECT i,'server-a','STATSZ','ok' FROM range(1,1751)t(i)`,
			`CHECKPOINT`,
			`INSERT INTO fatal_fixture SELECT i,'server-a','STATSZ','ok' FROM range(1751,2049)t(i)`,
			`PRAGMA disable_checkpoint_on_shutdown`,
		} {
			_, err := db.Exec(q)
			must(err)
		}
	case "replay":
		_, err := db.Exec("SELECT 1")
		must(err)
	default:
		panic("unknown mode")
	}
	must(db.Close())
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

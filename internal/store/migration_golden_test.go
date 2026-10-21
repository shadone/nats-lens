package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

// goldenMigrations pins every embedded migration's sha256. A migration that
// has been applied anywhere must never change: add a new migration instead.
var goldenMigrations = map[string]string{
	"0001_epochs_servers":                  "4f6baaffec1f433a357ee3bddba35890594b132d9e22912296528ba9b03f418d",
	"0002_server_stats":                    "2894f9862fd1daae5ba87e59f98cf19ba4deb47ff2675de702bbf4a5ecd49f06",
	"0003_connections_accounts":            "1aba3cf1286ccc2de10c61fa694e5ca1b7fc13388f0b637227b4ed51085dd8cf",
	"0004_streams":                         "abe1f6c38f993b21ce35f2c0c52bee632f0d939c80a34a5f62ecd2e584916337",
	"0005_consumers":                       "acac96ca1774e26c79134bbec2918896c27dc81311ebdce7e4ae318e41ea55bf",
	"0006_raft":                            "78d3f0c502acb5ca4f29101f6e42de00001a4caae483e0ccccefc8550d5982ac",
	"0007_events":                          "e192ad3a85bd704057b7207be327147cd6c68ca8df28f20d882866c460212c9c",
	"0008_archive_hours":                   "6554d7101048ed826a3700c7f8b790ddb9dfff380e15c38dbd1ad39129f9b910",
	"0009_config_runs":                     "0e775a3ac60526080daa60c721fa1fbb154ec748f6046bf24ff4a204ce371581",
	"0010_migration_checksums":             "bc8afcb6df5098e5ca3dd11446d673d5c45bd86761d49668e7b3c9d65de18de8",
	"0011_consumer_leader_counters":        "be1c392addbdfbeb6b75011dfa74ff4b0d9851bed957c0332aa69afe2c50dc12",
	"0012_unique_keys":                     "c286a5f82aedd169c97ebe592a0ed996bc4d6053bc4ab1671651a566bd51f77b",
	"0013_wal_error_comment":               "2042ff0b22d22a5960b50ff77e1970e9a071c3df314d23b332c2a2c6ff64bea5",
	"0014_archive_counts":                  "c77672b3fbc96d1faa7abaaefe55d75f8c7d37a3b9e7b2b1d4d621f0b2b57646",
	"0015_leader_null_comment":             "9fbbed04f85c52e665175d4ec667e0dff399620ffc7045b613cf62075ea27f6b",
	"0016_mutes":                           "231f3878a479dbbddf8634dc7adf05626c26deff432c78869712a9c51515e57b",
	"0017_archive_mute_changes":            "bc80d8513e9da8a85b7e4bcc48a4a0b254e0cbbe79862f70c2373f2e3fe621b1",
	"0018_findings":                        "83d4dbceb2518428cfb1a499a26203874670a23c2bd369ab924060eda0af4464",
	"0019_finding_events":                  "b5ad9e8b8267410c1d8813f8ae99285540c72aaa297f8cc2ccd7f6286c6331ad",
	"0020_flapping_cycles":                 "c7f0ad15b509987a6bbfa3caaeaf7d8ebf1bb0dc22429248143695e93063e0d0",
	"0021_k8s_events_comment":              "7ef2c581cd7de5aa0f5be807df73f4adb22a386705190eb1934af5e90e283d4c",
	"0022_config_hash_without_created":     "4d6ad805e75528c2c4b57a4e75b214b6a527e59bc915115ff45e0533531f564a",
	"0023_finding_condition_since":         "b7d0b439d2e948b81de7abb30327412cd3885742626d906e088948ab2d83990c",
	"0024_epoch_loopback_tooling":          "efdbf768e7cc3bf501e9f15317dc4a17c79eeb7a46077e61202795d994be207b",
	"0025_server_limits":                   "5490f28d075b4c255b9572195027c73da1d2911c6e92945feaa6960a7144669f",
	"0026_leafnodes_is_spoke_comment":      "45c7501c732e0df8aaab40444713ee42d53857d5e3a88a55d4ccdd48445bbdc6",
	"0027_account_details":                 "515152e0f135574571bda6dbaddf433c87d41b1b73ba2d97d1e0fe5d6cf29540",
	"0028_cert_and_jwt_expiry":             "919419f38977405fd301ea9ec17ce96ad72cd9e0fffd783840a10510ff7085e2",
	"0029_account_limit_and_cert_comments": "32a9e4836feba3c200e28ef500dd3d1602fb48ff5b0cfdf1196e37831f5a16a2",
	"0030_account_consumer_limit_comment":  "f46781815a4fdeb2e3d061eefb8b0d8c6d25190303efd97dae586dc0cf5c01f7",
	"0031_accepted_findings":               "a8a2e120e0226fcd299a548c13c61b5e15ca4b6715180d2f31072823a65ced9d",
}

func TestMigrationsMatchGolden(t *testing.T) {
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, m := range all {
		key := fmt.Sprintf("%04d_%s", m.version, m.name)
		seen[key] = true
		want, ok := goldenMigrations[key]
		switch {
		case !ok:
			t.Errorf("migration %s is new: add its sha256 %s to goldenMigrations", key, m.sha256)
		case want != m.sha256:
			t.Errorf("migration %s changed (sha256 %s, pinned %s): never edit a migration, add a new migration instead", key, m.sha256, want)
		}
	}
	for key := range goldenMigrations {
		if !seen[key] {
			t.Errorf("migration %s was removed: never remove a migration, add a new migration instead", key)
		}
	}
}

// A database that recorded a migration's checksum before an accepted edit
// opens, and its recorded checksum becomes the file's current one.
func TestAcceptedEarlierChecksumOpens(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db.duckdb")
	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	const old = "8c3fdcfe0e5ae37dca68b8fc991976a3e77f8ef1407bb547a029b4d123faec53"
	if _, err := s.DB().Exec(`UPDATE migrations SET sha256 = ? WHERE version = 18`, old); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenPath(ctx, path)
	if err != nil {
		t.Fatalf("database at the earlier 0018 checksum: %v", err)
	}
	defer s.Close()
	var got string
	if err := s.DB().QueryRow(`SELECT sha256 FROM migrations WHERE version = 18`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != goldenMigrations["0018_findings"] {
		t.Errorf("recorded checksum %s, want the current %s", got, goldenMigrations["0018_findings"])
	}
}

package offline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/dataset"
	"github.com/shadone/nats-lens/internal/investigation"
	"github.com/shadone/nats-lens/internal/store"
)

// These are published-run fixtures, not executions of the SQL check engine.
// Configured providers receive the real complete embedded catalogue. Public
// offline sources deliberately receive no configuration, which is tested below.
func TestConfiguredSummaryRequiresEveryApplicableCheck(t *testing.T) {
	for _, scenario := range []struct{ name, want string }{
		{"one_ok", "STATUS_NO_DATA"}, {"one_error", "STATUS_NO_DATA"},
		{"all_ok", "STATUS_OK"}, {"missing_replaced_by_unrelated", "STATUS_NO_DATA"}, {"one_missing", "STATUS_NO_DATA"}, {"one_error_among_ok", "STATUS_NO_DATA"},
		{"known_disabled_with_ok", "STATUS_OK"}, {"known_disabled_missing", "STATUS_NO_DATA"}, {"all_known_disabled", "STATUS_OK"},
		{"complete_previous_epoch_only", "STATUS_NO_DATA"}, {"previous_error_current_success", "STATUS_OK"},
		{"server_complete_other_types_error", "STATUS_NO_DATA"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := context.Background()
			path := closedFixture(t)
			set, err := checks.Load(config.Checks{})
			if err != nil {
				t.Fatal(err)
			}
			if len(set.Checks()) < 90 {
				t.Fatalf("requires the full shipped catalogue, got %d", len(set.Checks()))
			}
			disabled := []string{}
			if strings.HasPrefix(scenario.name, "known_disabled") {
				disabled = []string{"server-001"}
			}
			if scenario.name == "all_known_disabled" {
				for _, check := range set.Checks() {
					disabled = append(disabled, check.Code)
				}
			}
			set, err = checks.Load(config.Checks{Disabled: disabled})
			if err != nil {
				t.Fatal(err)
			}
			st, err := store.OpenPath(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			const selected int64 = 9007199254740993
			if _, err = st.DB().Exec(`INSERT INTO epochs BY NAME SELECT * REPLACE (9007199254740992 AS epoch,started_at-INTERVAL 30 SECOND AS started_at,finished_at-INTERVAL 30 SECOND AS finished_at) FROM epochs`); err != nil {
				t.Fatal(err)
			}
			tx, err := st.DB().BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			add := func(epoch int64, code, status string) {
				t.Helper()
				if _, err := tx.ExecContext(ctx, `INSERT INTO check_runs(epoch,check_code,status,rows,duration_ms) VALUES (?,?,?,0,1)`, epoch, code, status); err != nil {
					t.Fatal(err)
				}
			}
			for _, check := range set.Checks() {
				code := check.Code
				status := "ok"
				epoch := selected
				switch scenario.name {
				case "one_ok", "one_error":
					if code != "server-001" {
						continue
					}
					if scenario.name == "one_error" {
						status = "error"
					}
				case "one_missing", "missing_replaced_by_unrelated":
					if code == "server-001" {
						continue
					}
				case "one_error_among_ok":
					if code == "server-001" {
						status = "error"
					}
				case "known_disabled_with_ok":
					if set.Disabled(code) {
						status = "disabled"
					}
				case "known_disabled_missing":
					if set.Disabled(code) {
						continue
					}
				case "all_known_disabled":
					status = "disabled"
				case "complete_previous_epoch_only":
					epoch = selected - 1
				case "server_complete_other_types_error":
					if check.Entity != "server" {
						status = "error"
					}
				}
				add(epoch, code, status)
			}
			if scenario.name == "missing_replaced_by_unrelated" {
				add(selected, "stream-999", "ok")
			}
			if scenario.name == "previous_error_current_success" {
				add(selected-1, "server-001", "error")
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if err = st.Close(); err != nil {
				t.Fatal(err)
			}
			db, err := dataset.OpenDatabase(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			view, err := store.NewReadView(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			reference := time.Date(2026, 10, 17, 14, 29, 1, 0, time.UTC)
			fingerprint, err := dataset.FingerprintDatabase(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			captured, err := storeprov.NewCaptured(ctx, db, 30*time.Second, reference, fingerprint)
			if err != nil {
				t.Fatal(err)
			}
			// Explicit test-supplied known metadata; public Source.Open never invents it.
			captured.Checks = set
			live := &storeprov.Provider{St: view, Checks: set, Live: true, Interval: 30 * time.Second, Clock: func() time.Time { return reference }}
			for _, backend := range []struct {
				name     string
				provider api.Provider
			}{{"configured_live", live}, {"configured_captured", captured}} {
				t.Run(backend.name, func(t *testing.T) {
					source := investigation.NewInProcess(api.NewInvestigationHandler(&api.Handler{Systems: api.One("shop", "", backend.provider)}))
					data, err := source.Query(ctx, "ListSystems", []byte(`{"stripBuckets":1}`))
					if err != nil {
						t.Fatal(err)
					}
					var result struct {
						Systems []struct {
							Status string `json:"status"`
						} `json:"systems"`
					}
					if err = json.Unmarshal(data, &result); err != nil {
						t.Fatal(err)
					}
					if len(result.Systems) != 1 || result.Systems[0].Status != scenario.want {
						t.Fatalf("%d configured checks, %s: %s", len(set.Checks()), scenario.name, data)
					}
					catalog, err := source.Query(ctx, "ListCheckDefinitions", []byte(`{}`))
					if err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(string(catalog), `"configurationState":"configured"`) {
						t.Fatalf("supplied metadata lost: %s", catalog)
					}
					if scenario.name == "server_complete_other_types_error" {
						servers, err := source.Query(ctx, "ListServers", []byte(`{}`))
						if err != nil {
							t.Fatal(err)
						}
						if !strings.Contains(string(servers), `"status":"STATUS_OK"`) {
							t.Fatalf("entity-type coverage changed: %s", servers)
						}
					}
					if _, err = source.Query(ctx, "GetOverview", []byte(`{"system":"other"}`)); err == nil {
						t.Fatal("wrong system accepted")
					}
				})
			}
		})
	}
}

func TestCapturedSummaryKeepsUnavailableConfigurationExplicit(t *testing.T) {
	ctx := context.Background()
	path := closedFixture(t)
	st, err := store.OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.DB().Exec(`INSERT INTO check_runs(epoch,check_code,status,rows,duration_ms) VALUES (9007199254740993,'server-001','ok',0,1)`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	session, err := Open(ctx, investigation.OfflineSelection{DB: path, System: "shop", Interval: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	data, err := session.Source.Query(ctx, "ListCheckDefinitions", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"configurationState":"unavailable"`) {
		t.Fatalf("configuration reconstructed: %s", data)
	}
	provenance, _ := json.Marshal(session.Provenance)
	if !strings.Contains(string(provenance), "check overrides are unavailable") {
		t.Fatalf("missing limitation: %s", provenance)
	}
}

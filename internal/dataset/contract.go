package dataset

import (
	"bytes"
	"encoding/json"
	"io"
)

// Version 1 disclosures describe the full retained system, including unavoidable
// unknown pipeline/buffer state. Both writer and reader use this same contract.
func pendingStagesV1() []string {
	return []string{"collection_and_event_buffers_unknown", "evaluation_pipeline_completion_unknown"}
}
func exportedFactsV1() []string {
	return []string{"all_retained_database_tables_and_catalog", "findings_and_check_runs_as_published", "durable_mutes_and_acknowledgements", "inventory_and_durable_notification_state", "archive_watermarks_without_archive_files"}
}
func liveOnlyV1() []string {
	return []string{"credentials_and_runtime_configuration", "connections_and_readiness", "collection_event_and_notification_callbacks", "hourly_archive_files", "in_memory_buffers_and_in_flight_evaluations"}
}

// validateContractV1 checks presence and disclosure meaning, not authenticity.
// Nullable observation fields must be present so unknown facts are explicit.
// Configured retention alone is optional and can be omitted or null.
func validateContractV1(raw []byte, m Manifest) error {
	top, err := requiredObject(raw, []string{"format_version", "scope", "system", "application_version", "schema_version", "engine_version", "capture_started_at", "exported_at", "boundaries", "complete", "pending_stages", "exported_facts", "live_only", "checks_configuration", "data"}, nil)
	if err != nil {
		return err
	}
	if _, err = requiredObject(top["system"], []string{"id", "name", "collection_interval_ns"}, nil, "retention_ns"); err != nil {
		return err
	}
	if _, err = requiredObject(top["data"], []string{"path", "size", "sha256"}, nil); err != nil {
		return err
	}
	nullable := []string{"first_epoch", "latest_epoch", "first_collection_at", "last_collection_started_at", "data_reference", "first_event_at", "last_event_at", "last_event_received_at", "latest_event_id", "latest_evaluated_epoch"}
	fields := append(append([]string{}, nullable...), "check_runs", "pending_evaluation_epochs")
	if _, err = requiredObject(top["boundaries"], fields, nullable); err != nil {
		return err
	}
	if !safeVersion.MatchString(m.ApplicationVersion) || !safeVersion.MatchString(m.EngineVersion) || m.SchemaVersion <= 0 || len(m.System.ID) > 128 || len(m.System.Name) > 512 || m.System.RetentionNS != nil && *m.System.RetentionNS <= 0 {
		return ErrInvalid
	}
	if !sameTokens(m.PendingStages, pendingStagesV1()) || !sameTokens(m.ExportedFacts, exportedFactsV1()) || !sameTokens(m.LiveOnly, liveOnlyV1()) {
		return ErrInvalid
	}
	return nil
}

func sameTokens(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	remaining := make(map[string]bool, len(want))
	for _, s := range want {
		remaining[s] = true
	}
	for _, s := range got {
		if !remaining[s] {
			return false
		}
		delete(remaining, s)
	}
	return len(remaining) == 0
}

// Read each object once to reject duplicate keys rather than silently adopting
// the last value. Typed decoding separately checks field types and unknown keys.
func requiredObject(raw []byte, required, nullable []string, optional ...string) (map[string]json.RawMessage, error) {
	allowed := map[string]bool{}
	for _, name := range required {
		allowed[name] = true
	}
	for _, name := range optional {
		allowed[name] = true
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrInvalid
	}
	fields := map[string]json.RawMessage{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, ErrInvalid
		}
		name, ok := key.(string)
		if !ok || !allowed[name] {
			return nil, ErrInvalid
		}
		if _, duplicate := fields[name]; duplicate {
			return nil, ErrInvalid
		}
		var value json.RawMessage
		if dec.Decode(&value) != nil {
			return nil, ErrInvalid
		}
		fields[name] = value
	}
	if token, err = dec.Token(); err != nil || token != json.Delim('}') {
		return nil, ErrInvalid
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, ErrInvalid
	}
	allowsNull := map[string]bool{}
	for _, key := range nullable {
		allowsNull[key] = true
	}
	for _, key := range required {
		value, ok := fields[key]
		if !ok || !allowsNull[key] && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, ErrInvalid
		}
	}
	return fields, nil
}

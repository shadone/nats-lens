package storeprov

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

type storageServer struct {
	ID              string `json:"server_id"`
	Name            string `json:"server_name"`
	Enabled         *bool  `json:"enabled"`
	File            *int64 `json:"file_bytes"`
	Memory          *int64 `json:"memory_bytes"`
	JSZ             bool   `json:"jsz_ok"`
	ExpectedStreams *int64 `json:"expected_streams"`
	RecordedStreams int64  `json:"recorded_streams"`
}
type storageReport struct {
	Account     string `json:"account"`
	Stream      string `json:"stream"`
	ID          string `json:"server_id"`
	Created     *int64 `json:"created_us"`
	Bytes       *int64 `json:"bytes"`
	Leader      *bool  `json:"is_leader"`
	Group       string `json:"raft_group"`
	StorageMin  string `json:"storage_min"`
	StorageMax  string `json:"storage_max"`
	ReplicasMin *int32 `json:"replicas_min"`
	ReplicasMax *int32 `json:"replicas_max"`
}
type storageSnapshot struct {
	jsByID                       map[string]bool
	assets                       map[string][]string
	endpoint                     *v1.StorageChangeEndpoint
	expected                     []string
	servers                      map[string][]storageServer
	groups                       map[string][]storageReport
	expectedCount, observedCount int
}
type storageEvent struct {
	ID      int64  `json:"id"`
	Account string `json:"account"`
	Stream  string `json:"stream"`
	Kind    string `json:"kind"`
	Time    int64  `json:"time_us"`
}

// StorageChange reads completed endpoint windows, configurations, reports and
// bounded lifecycle evidence from one transaction. No history-wide enrichment.
func (p *Provider) StorageChange(ctx context.Context, req *v1.GetStorageChangeRequest) (*api.StorageChangeFacts, error) {
	if req.GetA() == nil || req.GetB() == nil || req.A.CheckValid() != nil || req.B.CheckValid() != nil || !req.A.AsTime().Before(req.B.AsTime()) || req.B.AsTime().After(p.RangeReference()) || req.EpochA < 0 || req.EpochB < 0 {
		return nil, fmt.Errorf("%w: valid ordered nonfuture a and b and nonnegative pins are required", api.ErrInvalid)
	}
	pinA, pinB := req.EpochA, req.EpochB
	if req.PageToken != "" {
		token, err := api.DecodeStorageCursor(req.PageToken)
		if err != nil {
			return nil, err
		}
		if pinA != 0 && pinA != token.A || pinB != 0 && pinB != token.B {
			return nil, fmt.Errorf("%w: continuation pins differ", api.ErrInvalid)
		}
		pinA, pinB = token.A, token.B
	}
	tx, err := p.St.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	budget := newMetadataBudget()
	a, err := readStorageSnapshot(ctx, tx, budget, req.A.AsTime().UTC(), pinA, p.Interval, req.Account, p.Now())
	if err != nil {
		return nil, err
	}
	b, err := readStorageSnapshot(ctx, tx, budget, req.B.AsTime().UTC(), pinB, p.Interval, req.Account, p.Now())
	if err != nil {
		return nil, err
	}
	f := &api.StorageChangeFacts{A: a.endpoint, B: b.endpoint, LocalOnly: p.CapturedOnly}
	var events []storageEvent
	if a.endpoint.Epoch != 0 && b.endpoint.Epoch != 0 {
		err = readMetadataOn(ctx, tx, `SELECT id,account,stream,kind,epoch_us(time) AS time_us FROM events e
WHERE kind IN ('stream.deleted','stream.created','stream.updated') AND consumer IS NULL
 AND time >= ? AND time <= ? AND received_at <= ? AND (? = '' OR account = ?)
 AND EXISTS(SELECT 1 FROM stream_stats x WHERE x.epoch IN (?,?) AND x.account=e.account AND x.stream=e.stream)
ORDER BY time,id`, []any{a.endpoint.FinishedAt.AsTime(), b.endpoint.FinishedAt.AsTime(), b.endpoint.FinishedAt.AsTime(), req.Account, req.Account, a.endpoint.Epoch, b.endpoint.Epoch}, budget, func(raw []byte) error {
			var e storageEvent
			if err := json.Unmarshal(raw, &e); err != nil {
				return err
			}
			events = append(events, e)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	eventsByAsset := map[string][]storageEvent{}
	for _, e := range events {
		key := storageAssetKey(e.Account, e.Stream)
		eventsByAsset[key] = append(eventsByAsset[key], e)
	}
	for _, name := range storageNames(a, b) {
		for _, storage := range []string{"file", "memory"} {
			row := &v1.StorageChangeRow{Id: "server:" + base64.RawURLEncoding.EncodeToString([]byte(name)) + ":" + storage, RowKind: "server", Server: name, Storage: storage}
			row.Before, row.ProcessesA = storageServerValue(a, name, storage)
			row.After, row.ProcessesB = storageServerValue(b, name, storage)
			if row.Before.ServerReported == nil || row.After.ServerReported == nil {
				row.Reasons = append(row.Reasons, "server_measurement_unavailable")
			}
			if len(row.ProcessesA) == 1 && len(row.ProcessesB) == 1 && !slices.Equal(row.ProcessesA, row.ProcessesB) {
				row.Reasons = append(row.Reasons, "server_process_changed")
			}
			f.Rows = append(f.Rows, row)
		}
	}
	keys := map[string]bool{}
	for k := range a.groups {
		keys[k] = true
	}
	for k := range b.groups {
		keys[k] = true
	}
	for key := range keys {
		ga, gb := a.groups[key], b.groups[key]
		example := ga
		if len(example) == 0 {
			example = gb
		}
		x := example[0]
		matchingEvents := eventsByAsset[storageAssetKey(x.Account, x.Stream)]
		stores := map[string]bool{}
		for _, g := range [][]storageReport{ga, gb} {
			for _, r := range g {
				if r.StorageMin == r.StorageMax && (r.StorageMin == "file" || r.StorageMin == "memory") {
					stores[r.StorageMin] = true
				} else {
					stores["file"], stores["memory"] = true, true
				}
			}
		}
		for storage := range stores {
			row := &v1.StorageChangeRow{Id: "resource:" + key + ":" + storage, RowKind: "resource", Account: x.Account, Stream: x.Stream, Kind: api.CapacityKind(x.Stream), Storage: storage}
			if x.Created != nil {
				row.Created = ts(time.UnixMicro(*x.Created).UTC())
			}
			switch row.Kind {
			case v1.CapacityStreamKind_CAPACITY_STREAM_KIND_KEY_VALUE:
				row.Bucket = x.Stream[3:]
			case v1.CapacityStreamKind_CAPACITY_STREAM_KIND_OBJECT_STORE:
				row.Bucket = x.Stream[4:]
			}
			row.Before, row.ProcessesA, row.ConfiguredReplicasA = storageResourceValue(a, ga, storage, &row.Reasons)
			row.After, row.ProcessesB, row.ConfiguredReplicasB = storageResourceValue(b, gb, storage, &row.Reasons)
			for _, e := range matchingEvents {
				if e.Account == x.Account && e.Stream == x.Stream {
					row.EventIds = append(row.EventIds, e.ID)
				}
			}
			// An exact-account delete advisory after A qualifies absence, not a failed
			// report. Unknown incarnation cannot be attributed to an advisory.
			if len(ga) > 0 && len(gb) == 0 && x.Created != nil {
				deleted := false
				for _, e := range matchingEvents {
					if e.Account == x.Account && e.Stream == x.Stream && e.Kind == "stream.deleted" && e.Time > a.endpoint.FinishedAt.AsTime().UnixMicro() {
						deleted = true
					}
				}
				if deleted {
					row.After = storageKnownZero()
					row.Reasons = append(row.Reasons, "observed_delete_event")
				} else if storageNewIncarnation(b, x, a.endpoint.FinishedAt.AsTime()) {
					row.After = storageKnownZero()
					row.Reasons = append(row.Reasons, "incarnation_replaced")
				} else {
					row.Reasons = append(row.Reasons, "not_observed_at_b")
				}
			}
			if len(ga) == 0 && len(gb) > 0 && x.Created != nil && a.endpoint.Epoch != 0 && *x.Created > a.endpoint.FinishedAt.AsTime().UnixMicro() && *x.Created <= b.endpoint.FinishedAt.AsTime().UnixMicro() {
				row.Before = storageKnownZero()
				row.Reasons = append(row.Reasons, "created_after_a")
			}
			if len(ga) == 0 && row.GetBefore().Logical == nil {
				row.Reasons = append(row.Reasons, "not_observed_at_a")
			}
			if row.ConfiguredReplicasA != nil && row.ConfiguredReplicasB != nil && *row.ConfiguredReplicasA != *row.ConfiguredReplicasB {
				row.Reasons = append(row.Reasons, "configured_replicas_changed")
			}
			if x.Created == nil {
				row.Delta = nil
				row.Reasons = append(row.Reasons, "incarnation_unknown")
			}
			if x.Created != nil && ((len(ga) > 0 && *x.Created > a.endpoint.FinishedAt.AsTime().UnixMicro()) || (len(gb) > 0 && *x.Created > b.endpoint.FinishedAt.AsTime().UnixMicro())) {
				row.Reasons = append(row.Reasons, "invalid_creation_time")
			}
			slices.Sort(row.Reasons)
			row.Reasons = slices.Compact(row.Reasons)
			f.Rows = append(f.Rows, row)
		}
	}
	if len(f.Rows) > api.MaxStorageChangeRows {
		return nil, fmt.Errorf("%w: storage comparison exceeds ranking limit", api.ErrResourceExhausted)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return f, nil
}

func readStorageSnapshot(ctx context.Context, db queryReader, budget *metadataBudget, requested time.Time, pin int64, interval time.Duration, account string, reference time.Time) (*storageSnapshot, error) {
	cutoff := requested
	if cutoff.After(reference) {
		cutoff = reference
	}
	s := &storageSnapshot{endpoint: &v1.StorageChangeEndpoint{Requested: ts(requested)}, servers: map[string][]storageServer{}, groups: map[string][]storageReport{}, jsByID: map[string]bool{}, assets: map[string][]string{}}
	q := `SELECT epoch,epoch_us(started_at) AS started_us,epoch_us(finished_at) AS finished_us,expected_names,expected_servers,observed_servers FROM epochs WHERE `
	args := []any{}
	if pin != 0 {
		q += `epoch=?`
		args = append(args, pin)
	} else {
		q += `finished_at <= ? AND finished_at >= started_at`
		args = append(args, cutoff)
	}
	q += ` ORDER BY finished_at DESC,started_at DESC,epoch DESC LIMIT 1`
	found := false
	err := readMetadataOn(ctx, db, q, args, budget, func(raw []byte) error {
		var row struct {
			Epoch         int64    `json:"epoch"`
			Started       int64    `json:"started_us"`
			Finished      int64    `json:"finished_us"`
			Expected      []string `json:"expected_names"`
			ExpectedCount int      `json:"expected_servers"`
			ObservedCount int      `json:"observed_servers"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		found = true
		start, finish := time.UnixMicro(row.Started).UTC(), time.UnixMicro(row.Finished).UTC()
		if finish.Before(start) || finish.After(cutoff) {
			return fmt.Errorf("%w: pinned collection window is after the requested moment or invalid", api.ErrInvalid)
		}
		s.endpoint.Epoch = row.Epoch
		s.endpoint.StartedAt = ts(start)
		s.endpoint.FinishedAt = ts(finish)
		s.endpoint.Age = durationpb.New(requested.Sub(finish))
		if requested.After(reference) {
			s.endpoint.Reasons = append(s.endpoint.Reasons, "exclusive_end_inclusion")
		}
		s.expected = row.Expected
		s.expectedCount = row.ExpectedCount
		s.observedCount = row.ObservedCount
		if interval <= 0 {
			interval = 30 * time.Second
		}
		if requested.Sub(finish) > 3*interval {
			s.endpoint.Reasons = append(s.endpoint.Reasons, "stale_collection")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !found {
		if pin != 0 {
			return nil, fmt.Errorf("%w: pinned collection is no longer retained", api.ErrFailedPrecondition)
		}
		s.endpoint.Reasons = []string{"no_collection"}
		return s, nil
	}
	err = readMetadataOn(ctx, db, `SELECT st.server_id,s.server_name,st.js_enabled AS enabled,st.js_storage AS file_bytes,st.js_memory AS memory_bytes,st.js_streams AS expected_streams,
 (SELECT count(*) FROM stream_stats x WHERE x.epoch=st.epoch AND x.server_id=st.server_id) AS recorded_streams,
 coalesce((SELECT bool_and(e.state='ok') FROM epoch_endpoints e WHERE e.epoch=st.epoch AND e.server_id=st.server_id AND e.endpoint='JSZ'),false) AS jsz_ok
FROM server_stats st LEFT JOIN servers s USING(server_id) WHERE st.epoch=? ORDER BY st.server_id`, []any{s.endpoint.Epoch}, budget, func(raw []byte) error {
		var r storageServer
		if err := json.Unmarshal(raw, &r); err != nil {
			return err
		}
		if r.Name == "" {
			r.Name = "unknown-process:" + r.ID
		}
		s.servers[r.Name] = append(s.servers[r.Name], r)
		if _, duplicate := s.jsByID[r.ID]; duplicate {
			s.jsByID[r.ID] = false
		} else {
			s.jsByID[r.ID] = r.JSZ
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	complete := len(s.servers) >= max(s.expectedCount, s.observedCount) && len(s.servers) > 0
	for _, name := range s.expected {
		if len(s.servers[name]) != 1 {
			complete = false
		}
	}
	for _, rs := range s.servers {
		if len(rs) != 1 {
			complete = false
			continue
		}
		r := rs[0]
		if r.Enabled == nil || *r.Enabled && (!r.JSZ || r.File == nil || r.Memory == nil) {
			complete = false
		}
	}
	detailed := true
	for _, rs := range s.servers {
		for _, r := range rs {
			if r.Enabled == nil || *r.Enabled && (!r.JSZ || r.ExpectedStreams == nil || *r.ExpectedStreams != r.RecordedStreams) || r.Enabled != nil && !*r.Enabled && r.RecordedStreams != 0 {
				detailed = false
			}
		}
	}
	s.endpoint.InventoryComplete = complete && detailed
	if !detailed {
		s.endpoint.Reasons = append(s.endpoint.Reasons, "resource_inventory_incomplete")
	}
	if !complete {
		s.endpoint.Reasons = append(s.endpoint.Reasons, "missing_jsz")
	}
	err = readMetadataOn(ctx, db, `WITH configs AS (
 SELECT account,stream,server_id,created,min(storage) AS storage_min,max(storage) AS storage_max,min(replicas) AS replicas_min,max(replicas) AS replicas_max
 FROM stream_config WHERE first_epoch <= ? AND last_epoch >= ? AND (?='' OR account=?) GROUP BY account,stream,server_id,created)
SELECT x.account,x.stream,x.server_id,epoch_us(x.created) AS created_us,x.bytes,x.is_leader,coalesce(x.raft_group,'') AS raft_group,
 coalesce(c.storage_min,'') AS storage_min,coalesce(c.storage_max,'') AS storage_max,c.replicas_min,c.replicas_max
FROM stream_stats x LEFT JOIN configs c ON c.account=x.account AND c.stream=x.stream AND c.server_id=x.server_id AND c.created IS NOT DISTINCT FROM x.created
WHERE x.epoch=? AND (?='' OR x.account=?) ORDER BY x.account,x.stream,x.server_id`, []any{s.endpoint.Epoch, s.endpoint.Epoch, account, account, s.endpoint.Epoch, account, account}, budget, func(raw []byte) error {
		var r storageReport
		if err := json.Unmarshal(raw, &r); err != nil {
			return err
		}
		key := storageResourceKey(r)
		if _, known := s.groups[key]; !known {
			s.assets[storageAssetKey(r.Account, r.Stream)] = append(s.assets[storageAssetKey(r.Account, r.Stream)], key)
		}
		s.groups[key] = append(s.groups[key], r)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}
func storageResourceKey(r storageReport) string {
	created := "unknown"
	if r.Created != nil {
		created = strconv.FormatInt(*r.Created, 10)
	}
	b, _ := json.Marshal([]string{r.Account, r.Stream, created})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func storageNames(a, b *storageSnapshot) []string {
	names := map[string]bool{}
	for _, s := range []*storageSnapshot{a, b} {
		for n := range s.servers {
			names[n] = true
		}
		for _, n := range s.expected {
			names[n] = true
		}
	}
	out := make([]string, 0, len(names))
	for n := range names {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}
func storageServerValue(s *storageSnapshot, name, storage string) (*v1.StorageChangeBytes, []string) {
	out := &v1.StorageChangeBytes{}
	rs := s.servers[name]
	ids := []string{}
	for _, r := range rs {
		ids = append(ids, r.ID)
	}
	slices.Sort(ids)
	if len(rs) != 1 {
		return out, ids
	}
	r := rs[0]
	if r.Enabled != nil && !*r.Enabled {
		out.ServerReported = proto.Int64(0)
	} else if r.JSZ && r.Enabled != nil && *r.Enabled {
		if storage == "file" {
			out.ServerReported = r.File
		} else {
			out.ServerReported = r.Memory
		}
	}
	return out, ids
}
func storageResourceValue(s *storageSnapshot, reports []storageReport, storage string, reasons *[]string) (*v1.StorageChangeBytes, []string, *int32) {
	out := &v1.StorageChangeBytes{}
	if len(reports) == 0 {
		return out, nil, nil
	}
	ids := []string{}
	groups := map[string]bool{}
	seen := map[string]bool{}
	values := []int64{}
	leaders := []*int64{}
	var replicas *int32
	actualStorage := ""
	valid := true
	copiesValid := true
	for _, r := range reports {
		ids = append(ids, r.ID)
		if seen[r.ID] {
			valid = false
		}
		seen[r.ID] = true
		if r.Group != "" {
			groups[r.Group] = true
		}
		if r.StorageMin == "" || r.StorageMin != r.StorageMax || (r.StorageMin != "file" && r.StorageMin != "memory") || (actualStorage != "" && actualStorage != r.StorageMin) {
			valid = false
		}
		actualStorage = r.StorageMin
		if r.ReplicasMin == nil || r.ReplicasMax == nil || *r.ReplicasMin != *r.ReplicasMax || *r.ReplicasMin < 1 || (replicas != nil && *replicas != *r.ReplicasMin) {
			valid = false
		} else {
			replicas = r.ReplicasMin
		}
		jsok := s.jsByID[r.ID]
		if r.Bytes == nil || *r.Bytes < 0 || !jsok {
			copiesValid = false
		} else {
			values = append(values, *r.Bytes)
		}
		if r.Leader != nil && *r.Leader {
			if jsok {
				leaders = append(leaders, r.Bytes)
			} else {
				leaders = append(leaders, nil)
			}
		}
	}
	slices.Sort(ids)
	if len(groups) > 1 || !valid {
		*reasons = append(*reasons, "ambiguous_configuration_or_identity")
		return out, ids, nil
	}
	if actualStorage != storage {
		return storageKnownZero(), ids, replicas
	}
	if len(leaders) == 1 {
		out.Logical = leaders[0]
	} else if len(leaders) == 0 && copiesValid && len(values) > 0 && slices.Min(values) == slices.Max(values) {
		out.Logical = proto.Int64(values[0])
		*reasons = append(*reasons, "no_leader_consistent_copies")
	} else {
		*reasons = append(*reasons, "leader_or_copy_ambiguity")
	}
	if copiesValid && replicas != nil && int(*replicas) == len(reports) {
		sum := int64(0)
		for _, v := range values {
			if sum > math.MaxInt64-v {
				copiesValid = false
				break
			}
			sum += v
		}
		if copiesValid {
			out.ObservedReplicas = &sum
		}
	} else {
		*reasons = append(*reasons, "replica_coverage_incomplete")
	}
	if out.Logical != nil && *out.Logical >= 0 && replicas != nil && *out.Logical <= math.MaxInt64/int64(*replicas) {
		v := *out.Logical * int64(*replicas)
		out.EstimatedReplicas = &v
	}
	return out, ids, replicas
}
func storageKnownZero() *v1.StorageChangeBytes {
	return &v1.StorageChangeBytes{Logical: proto.Int64(0), ObservedReplicas: proto.Int64(0), EstimatedReplicas: proto.Int64(0)}
}
func storageNewIncarnation(s *storageSnapshot, old storageReport, after time.Time) bool {
	if old.Created == nil || s.endpoint.Epoch == 0 {
		return false
	}
	var candidate *int64
	for _, key := range s.assets[storageAssetKey(old.Account, old.Stream)] {
		r := s.groups[key][0]
		if r.Account != old.Account || r.Stream != old.Stream {
			continue
		}
		if r.Created == nil || candidate != nil && *candidate != *r.Created {
			return false
		}
		candidate = r.Created
	}
	return candidate != nil && *candidate > *old.Created && *candidate > after.UnixMicro() && *candidate <= s.endpoint.FinishedAt.AsTime().UnixMicro()
}

func storageAssetKey(account, stream string) string {
	raw, _ := json.Marshal([]string{account, stream})
	return string(raw)
}

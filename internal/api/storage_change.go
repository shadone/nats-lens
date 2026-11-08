package api

import (
	"cmp"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
)

const MaxStorageChangeRows = 20000
const MaxStorageChangeSpan = 30 * 24 * time.Hour
const MaxStorageChangePage = 100
const MaxStorageChangePageBytes = 512 << 10

// StorageChangeFacts contains endpoint-pinned facts before ranking and paging.
// Providers leave missing measurements unset and qualify observed absence.
type StorageChangeFacts struct {
	LocalOnly bool
	A, B      *v1.StorageChangeEndpoint
	Rows      []*v1.StorageChangeRow
	Reasons   []string
}

type storageCursor struct {
	Version         int `json:"v"`
	A, B            int64
	Offset          int
	Scope, Evidence string
}

// FinishStorageChange applies the same arithmetic and page contract to live,
// captured and demo facts. It does not read or mutate the source database.
func FinishStorageChange(req *v1.GetStorageChangeRequest, f *StorageChangeFacts) (*v1.GetStorageChangeResponse, error) {
	if f == nil || f.A == nil || f.B == nil {
		return nil, fmt.Errorf("%w: storage endpoints are unavailable", ErrFailedPrecondition)
	}
	if len(f.Rows) > MaxStorageChangeRows {
		return nil, fmt.Errorf("%w: storage comparison ranking exceeds limit", ErrResourceExhausted)
	}
	limit := int(req.PageSize)
	if limit == 0 {
		limit = MaxStorageChangePage
	}
	if limit < 1 || limit > MaxStorageChangePage {
		return nil, fmt.Errorf("%w: page_size must be 1 to 100", ErrInvalid)
	}
	res := &v1.GetStorageChangeResponse{A: proto.Clone(f.A).(*v1.StorageChangeEndpoint), B: proto.Clone(f.B).(*v1.StorageChangeEndpoint), EndpointPolicy: "completed_at_or_before_requested", System: req.System, Account: req.Account, Unit: "bytes", Reasons: slices.Clone(f.Reasons)}
	seen := map[string]bool{}
	for _, x := range f.Rows {
		if x == nil || x.Id == "" || seen[x.Id] || (x.Storage != "file" && x.Storage != "memory") || (x.RowKind != "resource" && x.RowKind != "server") {
			return nil, fmt.Errorf("%w: invalid storage row identity", ErrFailedPrecondition)
		}
		seen[x.Id] = true
		r := proto.Clone(x).(*v1.StorageChangeRow)
		for _, value := range []*v1.StorageChangeBytes{r.Before, r.After} {
			for _, n := range storageNumbers(value) {
				if n != nil && *n < 0 {
					return nil, fmt.Errorf("%w: negative byte measurement", ErrFailedPrecondition)
				}
			}
		}
		r.Delta = storageDifference(r.Before, r.After)
		if slices.Contains(r.Reasons, "incarnation_unknown") || slices.Contains(r.Reasons, "invalid_creation_time") {
			r.Delta = &v1.StorageChangeBytes{}
		}
		res.Rows = append(res.Rows, r)
	}
	slices.SortFunc(res.Rows, func(a, b *v1.StorageChangeRow) int {
		if a.RowKind != b.RowKind {
			if a.RowKind == "resource" {
				return -1
			}
			return 1
		}
		av, bv := a.GetDelta().Logical, b.GetDelta().Logical
		if a.RowKind == "server" {
			av, bv = a.GetDelta().ServerReported, b.GetDelta().ServerReported
		}
		if av == nil && bv != nil {
			return 1
		}
		if av != nil && bv == nil {
			return -1
		}
		if av != nil && bv != nil {
			if c := cmp.Compare(storageAbs(*bv), storageAbs(*av)); c != 0 {
				return c
			}
		}
		return cmp.Compare(a.Id, b.Id)
	})
	res.TotalCount = int32(len(res.Rows))
	for _, storage := range []string{"file", "memory"} {
		total := &v1.StorageChangeTotals{Storage: storage}
		var err error
		total.Before, err = storageSum(res.Rows, storage, func(r *v1.StorageChangeRow) *v1.StorageChangeBytes { return r.Before }, true, res.A.InventoryComplete)
		if err != nil {
			return nil, err
		}
		total.After, err = storageSum(res.Rows, storage, func(r *v1.StorageChangeRow) *v1.StorageChangeBytes { return r.After }, true, res.B.InventoryComplete)
		if err != nil {
			return nil, err
		}
		total.Delta = storageDifference(total.Before, total.After)
		total.KnownContributions, err = storageContributions(res.Rows, storage)
		if err != nil {
			return nil, err
		}
		for _, r := range res.Rows {
			if r.Storage == storage && r.RowKind == "resource" && (r.GetDelta().Logical == nil || r.GetDelta().ObservedReplicas == nil) {
				total.UnknownRows++
			}
		}
		if req.Account == "" && total.Delta.ServerReported != nil && total.Delta.ObservedReplicas != nil {
			v, ok := storageAdd(*total.Delta.ServerReported, -*total.Delta.ObservedReplicas)
			if !ok {
				return nil, fmt.Errorf("%w: residual exceeds int64", ErrResourceExhausted)
			}
			total.Unreconciled = &v
		}
		res.Totals = append(res.Totals, total)
	}
	if req.Account != "" {
		res.Reasons = append(res.Reasons, "server_usage_is_system_wide")
	}
	canonical := proto.Clone(req).(*v1.GetStorageChangeRequest)
	canonical.PageToken = ""
	canonical.PageSize = int32(limit)
	canonical.EpochA = f.A.Epoch
	canonical.EpochB = f.B.Epoch
	scopeBytes, _ := proto.MarshalOptions{Deterministic: true}.Marshal(canonical)
	scope := storageHash(scopeBytes)
	evidenceBytes, _ := proto.MarshalOptions{Deterministic: true}.Marshal(res)
	evidence := storageHash(evidenceBytes)
	offset := 0
	if req.PageToken != "" {
		token, err := DecodeStorageCursor(req.PageToken)
		if err != nil {
			return nil, err
		}
		if token.Scope != scope || token.A != f.A.Epoch || token.B != f.B.Epoch {
			return nil, fmt.Errorf("%w: storage continuation scope differs", ErrInvalid)
		}
		if token.Evidence != evidence {
			return nil, fmt.Errorf("%w: retained storage evidence changed; restart comparison", ErrFailedPrecondition)
		}
		offset = token.Offset
		if offset >= len(res.Rows) {
			return nil, fmt.Errorf("%w: storage continuation offset is outside ranking", ErrInvalid)
		}
	}
	end := min(offset+limit, len(res.Rows))
	all := res.Rows
	res.Rows = all[offset:end]
	for _, total := range res.Totals {
		omitted := make([]*v1.StorageChangeRow, 0, len(all)-len(res.Rows))
		omitted = append(omitted, all[:offset]...)
		omitted = append(omitted, all[end:]...)
		var err error
		total.OmittedContributions, err = storageContributions(omitted, total.Storage)
		if err != nil {
			return nil, err
		}
	}
	if end < len(all) {
		data, _ := json.Marshal(storageCursor{Version: 1, A: f.A.Epoch, B: f.B.Epoch, Offset: end, Scope: scope, Evidence: evidence})
		res.NextPageToken = base64.RawURLEncoding.EncodeToString(data)
	}
	q := url.Values{"system": {req.System}, "account": {req.Account}, "from": {req.GetA().AsTime().UTC().Format(time.RFC3339Nano)}, "to": {req.GetB().AsTime().UTC().Format(time.RFC3339Nano)}}
	if f.A.Epoch > 0 {
		q.Set("epochA", strconv.FormatInt(f.A.Epoch, 10))
	}
	if f.B.Epoch > 0 {
		q.Set("epochB", strconv.FormatInt(f.B.Epoch, 10))
	}
	res.InvestigationUrl = "/capacity/storage-change?" + q.Encode()
	if f.LocalOnly {
		res.InvestigationUrl = ""
		res.Reasons = append(res.Reasons, "offline_ui_unavailable")
	}
	if !fitsEntityBytes(res, MaxStorageChangePageBytes) {
		return nil, fmt.Errorf("%w: storage page exceeds byte budget; reduce page_size", ErrResourceExhausted)
	}
	return res, nil
}

// DecodeStorageCursor validates only the transport shape. Source/scope/evidence
// checks remain mandatory in FinishStorageChange; cursors are not authorization.
func DecodeStorageCursor(raw string) (storageCursor, error) {
	var token storageCursor
	if len(raw) > 1024 {
		return token, fmt.Errorf("%w: storage continuation is too long", ErrInvalid)
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(data, &token) != nil || token.Version != 1 || token.A <= 0 || token.B <= 0 || token.Offset < 1 || token.Offset >= MaxStorageChangeRows || len(token.Scope) != 64 || len(token.Evidence) != 64 {
		return token, fmt.Errorf("%w: invalid storage continuation", ErrInvalid)
	}
	return token, nil
}
func storageHash(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func storageAbs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
func storageNumbers(x *v1.StorageChangeBytes) []*int64 {
	if x == nil {
		return []*int64{nil, nil, nil, nil}
	}
	return []*int64{x.Logical, x.ObservedReplicas, x.EstimatedReplicas, x.ServerReported}
}
func storageValues(x []*int64) *v1.StorageChangeBytes {
	return &v1.StorageChangeBytes{Logical: x[0], ObservedReplicas: x[1], EstimatedReplicas: x[2], ServerReported: x[3]}
}
func storageDifference(a, b *v1.StorageChangeBytes) *v1.StorageChangeBytes {
	av, bv := storageNumbers(a), storageNumbers(b)
	out := make([]*int64, 4)
	for i := range out {
		if av[i] != nil && bv[i] != nil {
			v := *bv[i] - *av[i]
			out[i] = &v
		}
	}
	return storageValues(out)
}
func storageAdd(a, b int64) (int64, bool) {
	if b > 0 && a > math.MaxInt64-b || b < 0 && a < math.MinInt64-b {
		return 0, false
	}
	return a + b, true
}
func storageSum(rows []*v1.StorageChangeRow, storage string, get func(*v1.StorageChangeRow) *v1.StorageChangeBytes, complete, available bool) (*v1.StorageChangeBytes, error) {
	sums := []int64{0, 0, 0, 0}
	known := []bool{available, available, available, available}
	for _, r := range rows {
		if r.Storage != storage {
			continue
		}
		ns := storageNumbers(get(r))
		for i, n := range ns {
			if (r.RowKind == "server") != (i == 3) {
				continue
			}
			if n == nil {
				if complete {
					known[i] = false
				}
				continue
			}
			v, ok := storageAdd(sums[i], *n)
			if !ok {
				return nil, fmt.Errorf("%w: storage sum exceeds int64", ErrResourceExhausted)
			}
			sums[i] = v
		}
	}
	out := make([]*int64, 4)
	for i := range out {
		if known[i] {
			v := sums[i]
			out[i] = &v
		}
	}
	return storageValues(out), nil
}

func storageContributions(rows []*v1.StorageChangeRow, storage string) (*v1.StorageChangeBytes, error) {
	sum, err := storageSum(rows, storage, func(r *v1.StorageChangeRow) *v1.StorageChangeBytes { return r.Delta }, false, true)
	if sum != nil {
		sum.ServerReported = nil
	}
	return sum, err
}

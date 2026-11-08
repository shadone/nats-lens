package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	MaxListRankingRows  = 20000
	MaxEntityPage       = 100
	MaxEntityPageBytes  = 512 << 10
	MaxListContextBytes = 2 << 20
	MaxListContexts     = 8
	ListContextLifetime = 5 * time.Minute
)

// EntitySnapshot freezes ordering, summary values and totals, without stream history.
// It is a bounded process-local traversal context, not a database snapshot.
type EntitySnapshot struct {
	Streams   []*v1.StreamSummary
	Consumers []*v1.ConsumerSummary
	Totals    *v1.StreamTotals
	Epoch     int64
	Scope     string
	Range     *v1.TimeRange
	created   time.Time
	id        string
}

// ASVS 15.4.1: serialize access to the bounded context cache.
type EntityPages struct {
	mu       sync.Mutex
	contexts []*EntitySnapshot
}

type entityToken struct {
	ID     string        `json:"id"`
	Offset int           `json:"offset"`
	Range  *v1.TimeRange `json:"range"`
}

// ASVS 1.5.2, 2.2.1: accept only bounded JSON cursor data and validate its fields.
func readEntityToken(token string) (entityToken, error) {
	var t entityToken
	if len(token) > 1024 {
		return t, invalid("page_token is too long")
	}
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return t, invalid("invalid list page_token")
	}
	if err = json.Unmarshal(b, &t); err != nil || len(t.ID) != 22 || t.Offset < 1 || t.Offset > MaxOffset || t.Range.GetStart() == nil || t.Range.GetEnd() == nil {
		return entityToken{}, invalid("invalid list page_token")
	}
	return t, nil
}

// EntityListRange pins omitted bounds to the token's absolute range. Explicit
// changed bounds are rejected by the context scope check in Begin.
func EntityListRange(r *v1.TimeRange, token string, now time.Time) (*v1.TimeRange, error) {
	if token != "" {
		t, err := readEntityToken(token)
		if err != nil {
			return nil, err
		}
		if r == nil {
			r = &v1.TimeRange{}
		} else {
			r = proto.Clone(r).(*v1.TimeRange)
		}
		if r.Start == nil {
			r.Start = t.Range.Start
		}
		if r.End == nil {
			r.End = t.Range.End
		}
	}
	return ResolveRange(r, now, DefaultRange)
}

// EntityLimit bounds the legacy limit field; negative values are invalid.
func EntityLimit(n int32) (int, error) {
	if n < 0 {
		return 0, invalid("limit must not be negative")
	}
	if n == 0 {
		return MaxEntityPage, nil
	}
	return min(int(n), MaxEntityPage), nil
}

// EntityScope binds a context to the complete normalized request except its
// page size and token, which callers clear before passing the clone here.
func EntityScope(m proto.Message) string {
	b, _ := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	return string(m.ProtoReflect().Descriptor().FullName()) + "\x00" + string(b)
}

func (p *EntityPages) Begin(ctx context.Context, token, scope string, load func() (*EntitySnapshot, error)) (*EntitySnapshot, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	now := time.Now()
	if token != "" {
		t, err := readEntityToken(token)
		if err != nil {
			return nil, 0, err
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, s := range p.contexts {
			if s.id != t.ID {
				continue
			}
			if s.Scope != scope {
				return nil, 0, invalid("page_token does not match list filters, system or range")
			}
			if now.Sub(s.created) >= ListContextLifetime {
				break
			}
			if t.Offset >= max(len(s.Streams), len(s.Consumers)) {
				return nil, 0, invalid("page_token offset is outside the list")
			}
			return s, t.Offset, nil
		}
		return nil, 0, fmt.Errorf("%w: list context expired or was evicted; read the first page again", ErrFailedPrecondition)
	}
	s, err := load()
	if err != nil {
		return nil, 0, err
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if max(len(s.Streams), len(s.Consumers)) > MaxListRankingRows {
		return nil, 0, fmt.Errorf("%w: list ranking exceeds %d rows; narrow filters", ErrResourceExhausted, MaxListRankingRows)
	}
	size := proto.Size(&v1.ListStreamsResponse{Streams: s.Streams, Totals: s.Totals}) + proto.Size(&v1.ListConsumersResponse{Consumers: s.Consumers})
	if size > MaxListContextBytes {
		return nil, 0, fmt.Errorf("%w: list ranking exceeds %d bytes; narrow the account or stream filter", ErrResourceExhausted, MaxListContextBytes)
	}
	// ASVS 11.5.1: use an unpredictable process-local context identifier.
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, 0, err
	}
	s.id, s.Scope, s.created = base64.RawURLEncoding.EncodeToString(id[:]), scope, now
	p.mu.Lock()
	defer p.mu.Unlock()
	kept := make([]*EntitySnapshot, 0, MaxListContexts)
	for _, old := range p.contexts {
		if now.Sub(old.created) < ListContextLifetime {
			kept = append(kept, old)
		}
	}
	if len(kept) >= MaxListContexts {
		copy(kept, kept[1:])
		kept = kept[:len(kept)-1]
	}
	p.contexts = append(kept, s)
	return s, 0, nil
}

func (s *EntitySnapshot) Next(offset int) string {
	if offset >= max(len(s.Streams), len(s.Consumers)) {
		return ""
	}
	b, _ := json.Marshal(entityToken{ID: s.id, Offset: offset, Range: s.Range})
	return base64.RawURLEncoding.EncodeToString(b)
}

// FitsEntityPage checks both wire encodings with space reserved for the token.
// One oversized entity is an explicit resource error, never a partial entity.
func FitsEntityPage(m proto.Message) bool {
	return fitsEntityBytes(m, MaxEntityPageBytes-2048)
}

func fitsEntityBytes(m proto.Message, budget int) bool {
	if proto.Size(m) > budget {
		return false
	}
	b, err := protojson.Marshal(m)
	return err == nil && len(b) <= budget
}

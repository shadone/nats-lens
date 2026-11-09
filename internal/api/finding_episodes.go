package api

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EpisodePages holds only a trusted source identity, not retained response data.
// Tokens are bounded untrusted navigation data, never authentication.
type EpisodePages struct {
	mu     sync.Mutex
	source string
	used   bool
}

func (p *EpisodePages) InitImmutable(fingerprint string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	b, err := hex.DecodeString(fingerprint)
	if err != nil || len(b) != sha256.Size || hex.EncodeToString(b) != fingerprint || p.used || p.source != "" {
		return invalid("invalid occurrence dataset identity")
	}
	p.source = "dataset:" + fingerprint
	return nil
}
func (p *EpisodePages) identity() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.used = true
	if p.source == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		p.source = "process:" + hex.EncodeToString(b)
	}
	return p.source, nil
}

type EpisodeCursor struct {
	Membership string    `json:"m"`
	Offset     int       `json:"o"`
	ReadEpoch  int64     `json:"e,string"`
	Total      int32     `json:"n"`
	From       time.Time `json:"f"`
	To         time.Time `json:"t"`
	Issued     int64     `json:"a,string"`
	Scope      string    `json:"s"`
	continued  bool
}

func decodeEpisodeCursor(token string) (EpisodeCursor, error) {
	var c EpisodeCursor
	if len(token) > 2048 || !strings.HasPrefix(token, "fep1.") {
		return c, invalid("invalid occurrence page_token")
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, "fep1."))
	if err != nil {
		return c, invalid("invalid occurrence page_token")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return EpisodeCursor{}, invalid("invalid occurrence page_token")
	}
	var extra any
	if d.Decode(&extra) != io.EOF || c.Offset < 0 || c.Offset > MaxOffset || c.ReadEpoch < 1 || c.Total < 1 || c.Offset > int(c.Total) || !c.From.Before(c.To) || len(c.Scope) != 64 || !validEpisodeMembership(c.Membership) {
		return EpisodeCursor{}, invalid("invalid occurrence page_token")
	}
	now := time.Now().Unix()
	if c.Issued <= 0 || c.Issued > now+1 || now-c.Issued > int64(ListContextLifetime/time.Second) {
		return EpisodeCursor{}, fmt.Errorf("%w: occurrence page expired; restart the list", ErrFailedPrecondition)
	}
	c.continued = true
	return c, nil
}

// Prepare pins omitted bounds from a continuation before resolving defaults.
// Scope binds the source, system and all selectors; size/offset may change.
func (p *EpisodePages) Prepare(r *v1.ListFindingEpisodesRequest, reference time.Time) (EpisodeCursor, error) {
	var c EpisodeCursor
	if r.PageSize < 0 || r.PageSize > 100 || r.FirstRetainedEpoch < 0 || r.TransitionOffset < 0 || r.TransitionOffset > MaxOffset {
		return c, invalid("invalid occurrence page bounds")
	}
	if r.PageSize == 0 {
		r.PageSize = 50
	}
	if len(r.FindingId) > 4096 || len(r.CheckCode) > 128 {
		return c, invalid("occurrence filter is too long")
	}
	if r.FirstRetainedEpoch > 0 && r.FindingId == "" {
		return c, invalid("finding_id is required for an occurrence")
	}
	if r.IncludeTransitions && (r.FindingId == "" || r.FirstRetainedEpoch == 0) {
		return c, invalid("raw transitions require one exact occurrence")
	}
	if !r.IncludeTransitions && r.TransitionOffset != 0 {
		return c, invalid("transition_offset requires raw transitions")
	}
	if err := checkEntity("entity", r.Entity, true); err != nil {
		return c, err
	}
	var err error
	if r.PageToken != "" {
		c, err = decodeEpisodeCursor(r.PageToken)
		if err != nil {
			return c, err
		}
		if c.Offset == 0 && !r.IncludeTransitions {
			return c, invalid("invalid occurrence page offset")
		}
		if r.Range == nil {
			r.Range = &v1.TimeRange{}
		} else {
			r.Range = proto.Clone(r.Range).(*v1.TimeRange)
		}
		if r.Range.Start == nil {
			r.Range.Start = timestamppb.New(c.From)
		}
		if r.Range.End == nil {
			r.Range.End = timestamppb.New(c.To)
		}
	}
	r.Range, err = ResolveRange(r.Range, reference, DefaultRange)
	if err != nil {
		return c, err
	}
	source, err := p.identity()
	if err != nil {
		return c, err
	}
	canonical := proto.Clone(r).(*v1.ListFindingEpisodesRequest)
	canonical.PageToken = ""
	canonical.PageSize = 0
	canonical.TransitionOffset = 0
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(canonical)
	if err != nil {
		return c, err
	}
	sum := sha256.Sum256(append(append([]byte("natslens occurrences v1\x00"), []byte(source)...), b...))
	scope := hex.EncodeToString(sum[:])
	if c.continued && c.Scope != scope {
		return c, invalid("occurrence page source or scope changed")
	}
	if !c.continued {
		c.Scope = scope
		c.Issued = time.Now().Unix()
		c.From = r.Range.Start.AsTime().UTC()
		c.To = r.Range.End.AsTime().UTC()
	}
	return c, nil
}

func (c EpisodeCursor) Next(readEpoch int64, total int32, offset int) string {
	c.ReadEpoch, c.Total, c.Offset = readEpoch, total, offset
	b, _ := json.Marshal(c)
	return "fep1." + base64.RawURLEncoding.EncodeToString(b)
}
func (c EpisodeCursor) CheckTotal(total int32, membership string) error {
	if c.continued && (c.Total != total || c.Membership != membership) {
		return fmt.Errorf("%w: retained occurrence set changed; restart the list", ErrFailedPrecondition)
	}
	return nil
}

// The native aggregate is a compact change detector, not authenticity or a
// persistent snapshot. Source identity and scope are checked independently.
func validEpisodeMembership(s string) bool {
	if len(s) < 3 || len(s) > 128 {
		return false
	}
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return false
	}
	for _, p := range parts {
		if len(p) == 0 {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

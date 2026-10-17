// Package inventory reads the inventory file: the streams, KV buckets,
// object stores and durable consumers each account is expected to have,
// with the settings that matter. The inventory checks compare it with what
// the servers report.
package inventory

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
)

// File is the inventory file.
type File struct {
	Accounts []Account `yaml:"accounts"`
}

// Account lists what one account holds. Name is the account name as the
// servers report it (the readable name in operator mode is accepted too).
type Account struct {
	Name         string   `yaml:"name"`
	Streams      []Stream `yaml:"streams"`
	KV           []Bucket `yaml:"kv"`
	ObjectStores []Bucket `yaml:"object_stores"`
}

// Stream is an expected stream and its durable consumers. Unset settings
// are not compared.
type Stream struct {
	Name              string           `yaml:"name"`
	Replicas          *int             `yaml:"replicas"`
	Retention         *string          `yaml:"retention"`
	Storage           *string          `yaml:"storage"`
	Discard           *string          `yaml:"discard"`
	MaxAge            *config.Duration `yaml:"max_age"`
	MaxBytes          *Bytes           `yaml:"max_bytes"`
	MaxMsgs           *int64           `yaml:"max_msgs"`
	MaxMsgsPerSubject *int64           `yaml:"max_msgs_per_subject"`
	Consumers         []Consumer       `yaml:"consumers"`
}

// Bucket is an expected KV bucket or object store.
type Bucket struct {
	Name     string  `yaml:"name"`
	Replicas *int    `yaml:"replicas"`
	Storage  *string `yaml:"storage"`
	// History is a KV bucket's history per key (max_msgs_per_subject).
	History *int64 `yaml:"history"`
	// TTL is the bucket's max_age.
	TTL      *config.Duration `yaml:"ttl"`
	MaxBytes *Bytes           `yaml:"max_bytes"`
}

// Consumer is an expected durable consumer.
type Consumer struct {
	Name          string           `yaml:"name"`
	Replicas      *int             `yaml:"replicas"`
	AckWait       *config.Duration `yaml:"ack_wait"`
	MaxDeliver    *int             `yaml:"max_deliver"`
	MaxAckPending *int             `yaml:"max_ack_pending"`
	Filter        *Subjects        `yaml:"filter"`
}

// Bytes is a byte size: a whole number, optionally with a KiB, MiB, GiB or
// TiB suffix, or -1 for unlimited.
type Bytes int64

// UnmarshalText reads a Bytes.
func (b *Bytes) UnmarshalText(text []byte) error {
	s := strings.TrimSpace(string(text))
	mult := int64(1)
	for _, suf := range []struct {
		s string
		m int64
	}{{"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}} {
		if n, ok := strings.CutSuffix(s, suf.s); ok {
			s, mult = strings.TrimSpace(n), suf.m
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || (n < 0 && !(n == -1 && mult == 1)) {
		return fmt.Errorf("%q is not a byte size (a whole number with an optional KiB, MiB, GiB or TiB suffix, or -1)", string(text))
	}
	*b = Bytes(n * mult)
	return nil
}

// Subjects is one subject filter or a list of them.
type Subjects []string

// UnmarshalYAML accepts a string or a list of strings.
func (s *Subjects) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*s = Subjects{n.Value}
		return nil
	}
	var list []string
	if err := n.Decode(&list); err != nil {
		return err
	}
	*s = list
	return nil
}

// Load reads the inventory file at path into rows for the store.
func Load(path string) ([]store.InventoryRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open inventory: %w", err)
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	var file File
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("parse inventory %s: %w", path, err)
	}
	rows, err := file.Rows()
	if err != nil {
		return nil, fmt.Errorf("inventory %s: %w", path, err)
	}
	return rows, nil
}

var enums = map[string][]string{
	"retention": {"limits", "interest", "workqueue"},
	"storage":   {"file", "memory"},
	"discard":   {"old", "new"},
}

func checkEnum(errs *[]error, where, field string, v *string) {
	if v == nil {
		return
	}
	for _, ok := range enums[field] {
		if *v == ok {
			return
		}
	}
	*errs = append(*errs, fmt.Errorf("%s: %s %q is not one of %s", where, field, *v, strings.Join(enums[field], ", ")))
}

func dur(d *config.Duration) *time.Duration {
	if d == nil {
		return nil
	}
	v := d.D()
	return &v
}

func bytesOf(b *Bytes) *int64 {
	if b == nil {
		return nil
	}
	v := int64(*b)
	return &v
}

// Rows checks the file and returns one row per expected asset.
func (f File) Rows() ([]store.InventoryRow, error) {
	var errs []error
	var rows []store.InventoryRow
	seen := map[string]bool{}
	add := func(r store.InventoryRow) {
		key := r.Account + "/" + r.Stream + "/" + r.Consumer
		if seen[key] {
			errs = append(errs, fmt.Errorf("account %s: %s %s listed twice", r.Account, r.Kind, r.Name))
			return
		}
		seen[key] = true
		rows = append(rows, r)
	}
	for _, a := range f.Accounts {
		if a.Name == "" {
			errs = append(errs, errors.New("an account has no name"))
			continue
		}
		for _, s := range a.Streams {
			where := fmt.Sprintf("account %s stream %q", a.Name, s.Name)
			if s.Name == "" {
				errs = append(errs, fmt.Errorf("account %s: a stream has no name", a.Name))
				continue
			}
			checkEnum(&errs, where, "retention", s.Retention)
			checkEnum(&errs, where, "storage", s.Storage)
			checkEnum(&errs, where, "discard", s.Discard)
			add(store.InventoryRow{Account: a.Name, Kind: "stream", Name: s.Name, Stream: s.Name, Replicas: s.Replicas,
				Retention: s.Retention, Storage: s.Storage, Discard: s.Discard, MaxAge: dur(s.MaxAge),
				MaxBytes: bytesOf(s.MaxBytes), MaxMsgs: s.MaxMsgs, MaxMsgsPerSubject: s.MaxMsgsPerSubject})
			for _, c := range s.Consumers {
				if c.Name == "" {
					errs = append(errs, fmt.Errorf("%s: a consumer has no name", where))
					continue
				}
				r := store.InventoryRow{Account: a.Name, Kind: "consumer", Name: c.Name, Stream: s.Name, Consumer: c.Name,
					Replicas: c.Replicas, AckWait: dur(c.AckWait), MaxDeliver: c.MaxDeliver, MaxAckPending: c.MaxAckPending}
				if c.Filter != nil {
					r.FilterSubjects = append([]string{}, *c.Filter...)
					sort.Strings(r.FilterSubjects)
				}
				add(r)
			}
		}
		for _, b := range a.KV {
			if b.Name == "" {
				errs = append(errs, fmt.Errorf("account %s: a KV bucket has no name", a.Name))
				continue
			}
			checkEnum(&errs, fmt.Sprintf("account %s KV bucket %q", a.Name, b.Name), "storage", b.Storage)
			add(store.InventoryRow{Account: a.Name, Kind: "kv", Name: b.Name, Stream: "KV_" + b.Name, Replicas: b.Replicas,
				Storage: b.Storage, MaxAge: dur(b.TTL), MaxBytes: bytesOf(b.MaxBytes), MaxMsgsPerSubject: b.History})
		}
		for _, b := range a.ObjectStores {
			if b.Name == "" {
				errs = append(errs, fmt.Errorf("account %s: an object store has no name", a.Name))
				continue
			}
			if b.History != nil {
				errs = append(errs, fmt.Errorf("account %s object store %q: history applies to KV buckets only", a.Name, b.Name))
			}
			checkEnum(&errs, fmt.Sprintf("account %s object store %q", a.Name, b.Name), "storage", b.Storage)
			add(store.InventoryRow{Account: a.Name, Kind: "object_store", Name: b.Name, Stream: "OBJ_" + b.Name,
				Replicas: b.Replicas, Storage: b.Storage, MaxAge: dur(b.TTL), MaxBytes: bytesOf(b.MaxBytes)})
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return rows, nil
}

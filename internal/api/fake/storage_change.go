package fake

import (
	"context"
	"fmt"
	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"time"
)

// StorageChange reuses Capacity's scenario measurements and process identities;
// it does not manufacture growth from a daily forecast.
func (p *Provider) StorageChange(ctx context.Context, req *v1.GetStorageChangeRequest) (*api.StorageChangeFacts, error) {
	s := p.scen()
	pins := []int64{req.EpochA, req.EpochB}
	if req.PageToken != "" {
		token, err := api.DecodeStorageCursor(req.PageToken)
		if err != nil {
			return nil, err
		}
		if pins[0] != 0 && pins[0] != token.A || pins[1] != 0 && pins[1] != token.B {
			return nil, api.ErrInvalid
		}
		pins = []int64{token.A, token.B}
	}
	endpoints := []*v1.StorageChangeEndpoint{}
	rows := map[string]*v1.StorageChangeRow{}
	for i, requested := range []time.Time{req.GetA().AsTime(), req.GetB().AsTime()} {
		e := &v1.StorageChangeEndpoint{Requested: ts(requested)}
		end := requested
		if pins[i] != 0 {
			last := s.lastEpoch()
			if pins[i] < 1 || pins[i] > s.epochNumber(last) {
				return nil, api.ErrFailedPrecondition
			}
			first := last.Add(-last.Sub(s.dataStart()) / sampleEvery * sampleEvery)
			end = first.Add(time.Duration(pins[i]-1) * sampleEvery)
			if end.After(requested) {
				return nil, api.ErrInvalid
			}
			if end.After(s.now) || end.Before(s.dataStart()) {
				return nil, api.ErrFailedPrecondition
			}
		}
		f, err := p.Capacity(ctx, &v1.GetCapacityRequest{Range: &v1.TimeRange{Start: ts(s.dataStart()), End: ts(end.Add(time.Microsecond))}})
		if err != nil {
			return nil, err
		}
		if pins[i] != 0 && f.Epoch != pins[i] {
			return nil, fmt.Errorf("%w: demo collection pin unavailable", api.ErrFailedPrecondition)
		}
		if f.Epoch == 0 {
			e.Reasons = []string{"no_collection"}
			endpoints = append(endpoints, e)
			continue
		}
		e.Epoch = f.Epoch
		e.StartedAt = ts(f.EpochAt)
		e.FinishedAt = ts(f.EpochAt)
		e.Age = durationpb.New(requested.Sub(f.EpochAt))
		e.InventoryComplete = true
		if requested.Sub(f.EpochAt) > 3*sampleEvery {
			e.Reasons = append(e.Reasons, "stale_collection")
		}
		for _, sv := range f.Servers {
			if !sv.Reported {
				e.InventoryComplete = false
			}
			for _, storage := range []string{"file", "memory"} {
				key := "server:" + sv.Name + ":" + storage
				r := rows[key]
				if r == nil {
					r = &v1.StorageChangeRow{Id: key, RowKind: "server", Server: sv.Name, Storage: storage}
					rows[key] = r
				}
				v := &v1.StorageChangeBytes{}
				if storage == "file" {
					v.ServerReported = sv.Used
				} else {
					r.Reasons = []string{"demo_memory_usage_unavailable"}
				}
				if i == 0 {
					r.Before = v
				} else {
					r.After = v
				}
				for _, sp := range s.sc.servers {
					if sp.name == sv.Name {
						id := s.processID(sp, f.EpochAt)
						if i == 0 {
							r.ProcessesA = []string{id}
						} else {
							r.ProcessesB = []string{id}
						}
					}
				}
			}
		}
		for _, x := range f.Streams {
			if req.Account != "" && req.Account != x.Account {
				continue
			}
			key := "resource:" + x.Account + "/" + x.Stream + ":" + x.Storage
			r := rows[key]
			if r == nil {
				r = &v1.StorageChangeRow{Id: key, RowKind: "resource", Account: x.Account, Stream: x.Stream, Storage: x.Storage, Kind: api.CapacityKind(x.Stream)}
				if r.Kind == v1.CapacityStreamKind_CAPACITY_STREAM_KIND_KEY_VALUE {
					r.Bucket = x.Stream[3:]
				} else if r.Kind == v1.CapacityStreamKind_CAPACITY_STREAM_KIND_OBJECT_STORE {
					r.Bucket = x.Stream[4:]
				}
				rows[key] = r
			}
			v := &v1.StorageChangeBytes{Logical: x.Bytes}
			ids := []string{}
			for _, sp := range s.sc.streams {
				if sp.name == x.Stream {
					r.Created = ts(s.createdAt(sp))
					for _, name := range s.sc.peers(sp) {
						for _, sv := range s.sc.servers {
							if sv.name == name && !s.offline(name, f.EpochAt) {
								ids = append(ids, s.processID(sv, f.EpochAt))
							}
						}
					}
				}
			}
			if x.Bytes != nil {
				estimated := *x.Bytes * int64(x.Replicas)
				v.EstimatedReplicas = &estimated
				if len(ids) == int(x.Replicas) {
					v.ObservedReplicas = proto.Int64(estimated)
				}
			}
			if i == 0 {
				r.Before = v
				r.ProcessesA = ids
				r.ConfiguredReplicasA = proto.Int32(x.Replicas)
			} else {
				r.After = v
				r.ProcessesB = ids
				r.ConfiguredReplicasB = proto.Int32(x.Replicas)
			}
		}
		if !e.InventoryComplete {
			e.Reasons = append(e.Reasons, "missing_jsz")
		}
		endpoints = append(endpoints, e)
	}
	f := &api.StorageChangeFacts{A: endpoints[0], B: endpoints[1]}
	for _, r := range rows {
		f.Rows = append(f.Rows, r)
	}
	return f, nil
}

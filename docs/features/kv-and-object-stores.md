# KV and object stores

- **Surfaces:** `ui`, `api`
- **Status:** planned
- **Related:** [streams.md](streams.md), [stream-checks.md](stream-checks.md), [inventory.md](inventory.md), [capacity.md](capacity.md)

## What it does

Planned; nothing here is built yet. KV buckets and object stores will get pages of their own, named and described in their own terms (bucket, key, history, TTL) instead of as the streams `KV_<bucket>` and `OBJ_<bucket>` behind them. Today they appear only in the Streams list with "Include KV and object stores", and the stream checks cover them. All scenarios below describe intended behaviour.

## Behavior and rules

- **KV Stores.** One row per bucket: its name, replicas, storage, history per key, TTL, whether per-key TTLs are allowed, the number of keys, its size, its growth per day, and its findings.
- **Object Stores.** One row per store: its name, replicas, storage, TTL, byte limit, the number of objects, its size, its growth per day, and its findings.
- **The same checks.** Bucket findings are the stream checks' findings for the bucket's stream, shown with the bucket's name.
- **Built on stored data.** The bucket's stream configuration and statistics are collected today; the number of keys is roughly the stream's subject count, which JSZ reports; deleted keys whose delete markers remain are counted too.

## Scenarios

### Find buckets without a bound

- **Surfaces:** `ui`
- **Given** the bucket `customer-info` has neither a TTL nor a byte limit
- **When** I open KV Stores
- **Then** `customer-info` shows its `stream-005` finding, "KV bucket customer-info: keys accumulate without bound; …", with its key count

### Find an object store without a copy

- **Surfaces:** `ui`
- **Given** the object store `image-output` has one replica in a three-server cluster
- **When** I open Object Stores
- **Then** `image-output` is marked as having a single replica

### List buckets from the API

- **Surfaces:** `api`
- **Given** `natslens run` serves its API
- **When** a client asks for the KV buckets
- **Then** it receives each bucket with its settings and key count

## Not supported / out of scope

- **No keys or values.** nats-lens does not read the keys, values or objects in a bucket.
- **No watcher view.** KV watchers are ephemeral consumers; they appear among the consumers with "Include ephemeral and ordered".

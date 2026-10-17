# Inventory

- **Surfaces:** `ui`, `cli`
- **Status:** shipped
- **Related:** [checks.md](checks.md), [stream-checks.md](stream-checks.md), [consumer-checks.md](consumer-checks.md), [streams.md](streams.md), [consumers.md](consumers.md)

## What it does

The inventory is an optional YAML file that lists, per account, the streams, durable consumers, KV buckets and object stores that must exist, with the settings that must have a given value. nats-lens compares it with what the servers report: a listed asset that no server reports is a critical finding, even when nats-lens never saw it, and a setting that differs is a warning. Without an inventory, nats-lens can only notice an asset that disappeared after it had seen it.

## Behavior and rules

- **One file, read at start.** `inventory_file` (or `-inventory-file`) names the file. nats-lens reads it on every start and replaces its stored inventory with it; without the setting it empties the stored inventory, so the inventory checks stop. A change to the file needs a restart.
- **The layout.** The file holds `accounts`, each with a `name` (the account name the servers report; the readable name is accepted too) and lists of `streams` (each with its durable `consumers`), `kv` buckets and `object_stores`. The project's `inventory.example.yaml` describes an online shop with this layout.
- **Only given settings are compared.** A setting left out is not checked, so the file states only what matters. A stream takes `replicas`, `retention`, `storage`, `discard`, `max_age`, `max_bytes`, `max_msgs` and `max_msgs_per_subject`. A consumer takes `replicas`, `ack_wait`, `max_deliver`, `max_ack_pending` and `filter` (one subject or a list). A KV bucket takes `replicas`, `storage`, `history`, `ttl` and `max_bytes`; an object store the same without `history`.
- **Strict values.** `retention` is `limits`, `interest` or `workqueue`; `storage` is `file` or `memory`; `discard` is `old` or `new`. Durations take `s`, `m`, `h` or `d`. Byte sizes are a whole number with an optional `KiB`, `MiB`, `GiB` or `TiB` suffix, or `-1` for unlimited; nats-lens accepts no decimal suffixes such as `GB`, because NATS tools read them differently. An unknown key, a wrong value, a missing name or an asset listed twice stops the start with an error that names it.
- **The checks it feeds.** `stream-011` (expected stream or bucket missing), `stream-012` (stream configuration differs from the inventory), `consumer-010` (expected consumer missing) and `consumer-011` (consumer configuration differs from the inventory).
- **The exemptions it grants.** A stream the inventory declares with `max_age: 0` and `max_bytes: -1`, both given, is unbounded on purpose, and `stream-005` does not report it. A listed KV bucket that allows per-key TTLs is also left out of `stream-005`. Listed streams and durables are left out of `stream-014` and `consumer-013`, because `stream-011` and `consumer-010` report them.
- **Who deleted it.** When the events hold the delete advisory of a missing asset, the finding names the time and the client that deleted it.
- **A server that is away is not a delete.** While a server that held the asset is restarting or not answering, a missing asset is info or a warning instead of critical while the ready servers that lack it are fewer than a quorum of its holders, unless a delete event was captured.
- **Present and expected in the UI.** For a stream the inventory names, the stream lists show its consumers as present out of expected (for example `4/5`). A consumer the inventory expects that no server reports is listed as missing, with no numbers.
- **Not archived.** The inventory is not part of the hourly archive; nats-lens reads the file again at every start.

## Scenarios

### Declare what must exist

- **Surfaces:** `cli`
- **Given** a copy of `inventory.example.yaml` adapted to my streams
- **When** I start nats-lens with `inventory_file: inventory.yaml`
- **Then** it logs "inventory loaded" with the number of assets
- **And** `stream-011`, `stream-012`, `consumer-010` and `consumer-011` judge my assets against it

### A durable the inventory expects is gone

- **Surfaces:** `ui`
- **Given** the inventory lists `resize-worker` on `order-jobs` in account `APP`
- **When** a client deletes `resize-worker`
- **Then** `consumer-010` "Expected consumer missing" fires as critical, naming the client and the time
- **And** the Streams page shows `order-jobs` with one consumer fewer than expected, and the Consumers page lists `resize-worker` as missing

### A setting drifts

- **Surfaces:** `ui`
- **Given** the inventory sets `max_deliver: 3` for `media-chunk-pool`
- **When** the service that creates the consumer starts with `max_deliver: 5`
- **Then** `consumer-011` fires as a warning, with "max_deliver 5 (inventory 3)" in the detail

### A typo in the file

- **Surfaces:** `cli`
- **Given** an inventory with `retention: work-queue`
- **When** nats-lens starts
- **Then** it stops with an error saying that `retention "work-queue"` is not one of `limits, interest, workqueue`

## Not supported / out of scope

- **No reload without a restart.** The file is read at start only.
- **No discovery.** nats-lens does not write an inventory from what it sees; a person writes the file.
- **No settings beyond the listed ones.** Subjects, placement, compression and the other stream settings cannot be declared yet.
- **No ephemeral consumers.** The inventory lists durable consumers only.
- **No inventory per environment in one file.** Each system reads its own `inventory_file`; one file cannot describe several systems.

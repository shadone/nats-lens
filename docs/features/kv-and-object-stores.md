# KV and object stores

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [streams.md](streams.md), [stream-checks.md](stream-checks.md), [inventory.md](inventory.md), [capacity.md](capacity.md), [placement.md](placement.md)

## What it does

KV Stores and Object Stores are pages of their own, which name and describe buckets in their own terms (bucket, keys, history, TTL, watchers) instead of as the streams `KV_<bucket>` and `OBJ_<bucket>` behind them. They answer: which buckets exist, how big they are and how fast they grow, where their replicas are, what bounds them, and whether a bucket the inventory expects is gone.

## Behavior and rules

The Streams list can include storage streams with bounded pages and totals across all matches ([Streams](streams.md)). The dedicated bucket list calls retain their separate list contract.

- **Capacity navigation.** Capacity labels KV buckets and object stores explicitly, shows their bucket names and underlying stream identities, and offers type filters over the loaded rows. Links open bucket details with the system, account and time range.

- **A bucket is its stream.** A stream named `KV_<bucket>` is a KV bucket, one named `OBJ_<bucket>` an object store. A stream such as `KVX_foo` is neither.
- **The leader's view.** Numbers come from the bucket stream's leader in the newest collection of the range (the largest value when no server leads), and the configuration from the leader's report.
- **KV Stores.** For each bucket: its status, name, account and description, replicas, placement, keys, history per key, TTL, bytes (with the byte limit beneath when there is one), growth per day, watchers, storage (with `s2` when compressed), and notes: missing, "per-key TTL" when the bucket allows per-key TTLs, the mirrored stream, "not in inventory" when an inventory exists and does not list it, and the codes of its firing findings.
- **Keys count deleted keys.** Keys is the stream's subject count, which counts a deleted or purged key until its marker is gone. The column's tooltip and the table's footnote say so.
- **Watchers.** The consumers on the bucket's stream, which for a KV bucket are its watchers, ordered consumers included; the column's tooltip says so. When the servers count consumers on the stream that their consumer detail does not include, watchers shows a dash.
- **Growth per day.** Bucket growth follows the [Streams observation bounds and six-hour requirement](streams.md). A bucket’s own measurements determine whether its rate is available.
- **Object Stores.** For each store: its status, name, account and description, replicas, placement, messages, bytes with the byte limit, growth per day, TTL, storage, and notes: missing, sealed, "not in inventory", and finding codes. Messages is the stream's message count: the objects' chunks plus one metadata message per object; the column's tooltip and footnote say so.
- **Placement.** One square per replica, the leader first and then the followers by server name, by the same rule as a stream's ([streams.md](streams.md)): leader, offline (the leader lists it as offline), missing (the leader lists it and the server reports nothing), disagrees (not listed by the leader), behind (listed as not current, even while it still holds an older configuration), disagrees (a configuration other than the leader's), in sync. Without a leader a square's state is unknown, or disagrees when the servers hold different configurations. A bucket configured with more replicas than servers report gets a missing square for each. A bucket without any replica reported shows a dash. A legend under the table draws the mark of each state that occurs.
- **Missing buckets.** A bucket the inventory lists (kinds `kv` and `object_store`) that no server reports is shown with "missing", its finding (usually `stream-011`), and a dash for every number. It is not linked, as there is no stream to open.
- **The same checks.** A bucket's status and findings are the stream checks' findings for its stream.
- **Worst first, sortable.** Rows come worst first, then by account and name. The status, the name and every numeric column sort on a click, and a second click reverses; the sort is kept in the URL (`?sort=bytes`, `?sort=-bytes`). A value that was not observed sorts last either way.
- **A dash, never 0.** A number nats-lens did not observe shows as "–".
- **Narrow screens.** The table scrolls sideways inside its card, with a shadow at each edge where it is cut; descriptions wrap and the placement squares stay on one line, so placement shows without scrolling.
- **Account scope.** Both pages apply the account scope chosen in the header.
- **Bucket details.** A present bucket opens its own KV or object-store detail route, named by bucket and account, with the corresponding sidebar and breadcrumb. Storage bytes and trends, replica reports, configuration, findings and events use the same stored evidence as the underlying stream. KV details show reported history per key and TTL; absent configuration remains unknown. Stored records include revisions and deletion markers; object-store messages include chunks and metadata, not an object count. Watchers are observed consumers, not a count of clients reading keys.
- **Consistent navigation.** Search, Capacity, Placement, findings, events and other resource links use bucket routes and readable bucket names. System and time context follow the link; bucket detail routes also preserve account context. Existing `KV_` and `OBJ_` stream links redirect to the resource page. A secondary “Underlying stream diagnostics” action opens the explicit low-level stream view; raw storage names are not needed for the primary workflow.

## Scenarios

### Find buckets without a bound

- **Surfaces:** `ui`
- **Given** the bucket `customer-info` has neither a TTL nor a byte limit
- **When** I open KV Stores
- **Then** `customer-info` shows TTL "none", no byte limit, and its `stream-005` code among the notes

### See an expected bucket that is gone

- **Surfaces:** `ui`
- **Given** the inventory lists the KV bucket `label-templates` and no server reports it
- **When** I open KV Stores
- **Then** `label-templates` is listed first with "missing" and `stream-011`, a dash in every number, and no link

### Spot a single replica

- **Surfaces:** `ui`
- **Given** the KV bucket `worker-presence` has one replica in a three-server cluster
- **When** I open KV Stores
- **Then** its row reads R 1 and its placement is one leader square

### Sort by size

- **Surfaces:** `ui`
- **Given** KV Stores lists a missing bucket and two present ones
- **When** I click Bytes
- **Then** the largest bucket comes first and the missing bucket last, and a second click puts the smallest present bucket first

### Open a bucket detail

- **Surfaces:** `ui`
- **Given** Object Stores lists `image-output`
- **When** I click its name
- **Then** the object-store page named `image-output` opens on the same system and range, with Object Stores in its breadcrumb

### Follow an existing storage-stream link

- **Surfaces:** `ui`
- **Given** an existing link names stream `KV_customer-info` in account APP
- **When** I open the link
- **Then** it redirects to the KV bucket `customer-info`, preserving the system and range
- **And** the secondary diagnostics action can still open the underlying stream explicitly

### List buckets from the API

- **Surfaces:** `api`
- **Given** `natslens run` serves its API
- **When** a client calls `ListKeyValueStores` or `ListObjectStores`, optionally with an account
- **Then** it receives each bucket with its settings, numbers, placement, status and findings, and the inventory's missing buckets with `present` unset

## Not supported / out of scope

- **No keys or values.** nats-lens does not read the keys, values or objects in a bucket, so it cannot count live keys or objects.
- **No expected settings for a missing bucket.** A missing bucket shows no replicas or limits, even when the inventory gives them; its finding names them.


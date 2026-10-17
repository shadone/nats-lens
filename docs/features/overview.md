# What nats-lens is

- **Surfaces:** `ui`, `api`, `cli`
- **Status:** shipped
- **Related:** [README.md](README.md), [design.md](../design.md), [monitoring-snapshots.md](monitoring-snapshots.md), [checks.md](checks.md), [overview-page.md](overview-page.md)

## What it does

nats-lens watches one NATS system and keeps a history of what every server reports about itself, its connections and its JetStream assets. It turns that history into findings: plain statements of what is wrong, on which entity, since when, and what to do about it. It shows all of it in a web UI that ships inside the same binary, and it never changes anything in NATS.

NATS on its own is a black box to most of the people who run it. The servers answer monitoring requests about the present moment, and they publish advisories that nobody keeps. nats-lens records both, so that an operator can answer questions such as these:

- Is anything going wrong right now, and how bad is it?
- Why did clients time out during a node upgrade? Which servers restarted, when did replicas lose quorum, and how long did they take to catch up?
- Is a server's disk or a stream filling up, and when will it be full at the current rate?
- Is a consumer's backlog growing, that is, filling faster than it drains?
- Why did a durable consumer disappear, from which server's view, and which client deleted it?
- Do all servers agree on each stream's and consumer's configuration, creation time and peers?
- Which client connections are slow, stalled or reconnecting, and which service owns them?

## Behavior and rules

- **One process does everything.** `natslens run` collects, captures events, runs the checks, archives the history, and serves the API and the UI. It needs a user of the system account and, for stream and consumer advisories, a user per JetStream account.
- **Read-only towards NATS.** nats-lens only subscribes and sends monitoring requests. It never publishes to an application subject, never restarts a server and never edits a stream. The only writes it makes are to its own database: mutes.
- **Every server's own view is kept.** nats-lens never merges the servers into one answer. A single-replica asset appears only on its host, and when servers disagree about an asset, the disagreement is visible and can be a finding.
- **Silence is never read as health.** A server that does not answer, a collection that misses a reply, or a period when nats-lens itself was not running is recorded as such and shown.
- **The nouns.** These are the product's words, each with one meaning (see the table of terms in [README.md](README.md)):
  - a **system** is one NATS deployment with its own system account;
  - a **server** is one `nats-server`, known by its stable name;
  - an **epoch**, called a **collection** in the UI, is one snapshot of every server;
  - an **asset** is a stream, consumer, KV bucket or object store;
  - an **event** is one thing that happened at a moment;
  - a **check** is a rule, and a **finding** is one check failing for one entity;
  - the **inventory** lists the assets that must exist;
  - a **mute** hides a finding for a while, with a reason;
  - a **tooling connection** is nats-lens's own connection or a client on a loopback address.
- **One binary, three surfaces.** The web UI (`ui`) and the Connect API (`api`) are served by `natslens run`. The commands and the configuration file (`cli`) start it, query its database and list its checks.

## Scenarios

### See whether anything is wrong

- **Surfaces:** `ui`
- **Given** nats-lens has collected for a while
- **When** I open the UI
- **Then** the Overview shows the worst current finding first, the number of findings per severity, the system's key numbers and a timeline of every check
- **And** with nothing firing, the card reads "Nothing needs attention"; when nothing has resolved or been muted in the range either, it says that checks may not have run yet and points to the status timeline

### Reconstruct a node upgrade

- **Surfaces:** `ui`
- **Given** nats-lens runs with the Kubernetes watcher on, and a node upgrade restarted the NATS servers
- **When** I set the time range around the upgrade
- **Then** the timeline shows the node drains and pod restarts above the NATS events and the finding transitions on one clock
- **And** each finding says when it fired and resolved, for example when a stream lost and regained quorum

### Ask the data a question nats-lens has no page for

- **Surfaces:** `cli`
- **Given** a stopped nats-lens and its database directory
- **When** I run `natslens sql -data-dir DIR "SELECT ..."`
- **Then** nats-lens prints the result of the query on its own tables

### Read findings from another tool

- **Surfaces:** `api`
- **Given** `natslens run` serves its API
- **When** a client calls `ListFindings`, or Prometheus scrapes `/metrics`
- **Then** it receives the findings with their severity, entity and state

## Not supported / out of scope

- **One system per process.** Watching several systems from one nats-lens is planned ([multiple-systems.md](multiple-systems.md)). Today each system needs its own nats-lens.
- **No write actions towards NATS.** nats-lens tells a person what to do; it never does it.
- **No alert delivery of its own.** Findings reach an alerting system through `/metrics` ([metrics-and-alerting.md](metrics-and-alerting.md)). Notifications are planned ([notifications.md](notifications.md)).
- **No authentication of its own.** nats-lens expects an identity-aware proxy in front of it when it is reachable beyond localhost ([running-nats-lens.md](running-nats-lens.md)).
- **No per-subject traffic.** NATS servers do not report message rates per subject, so nats-lens cannot show them ([subjects.md](subjects.md)).
- **No high availability.** nats-lens runs as one replica, because its embedded database has one writer.

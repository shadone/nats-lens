# Replay

- **Surfaces:** `ui`, `api`
- **Status:** partial — a step for replicas catching up is not built
- **Related:** [data-history.md](data-history.md), [history-and-archive.md](history-and-archive.md), [events.md](events.md), [event-capture.md](event-capture.md), [kubernetes-context.md](kubernetes-context.md), [placement.md](placement.md), [demo-mode.md](demo-mode.md), [navigation.md](navigation.md), [design.md](../design.md), [the build plan](../plans/replay.md)

## What it does

The Replay page, under Cluster in the sidebar, answers "what changed between these two moments, and in what order?" You pick two moments, A and B, on a status strip of the page's range. nats-lens then tells the story in between from its collections, NATS advisories, finding transitions and Kubernetes events: which nodes were drained and upgraded, which servers restarted or stopped, which leaders moved, which assets lost quorum or disappeared, and which findings fired and resolved. Every step links to its evidence. Beside the story, nats-lens counts what changed and lists the streams and durable consumers that servers held at A and no server holds at B. You can copy a link to the same A and B, or export the story as a text incident log. `GetReplay` serves the same story to any API client.

## Behavior and rules

- **Chart descriptions.** The Replay time selector has [chart help](chart-help.md) describing snapshot comparisons, state buckets and inferred windows.

- **First visit.** The page reads data for the current scope while its view loads. The shell stays available, with loading and reload recovery described in [navigation](navigation.md).

- **Observed client libraries.** Replay compares the client-library sets in the collections near A and B, grouped by the application name before `@` and account. A changed set produces a step spanning the two observation times, with both version mixes and connection counts. Mixed rolling versions remain visible. Count changes within the same library set do not make a change step. These are endpoint observations: an intermediate change can be missed, and the span does not date a deployment. Client library metadata does not establish an application release version, build SHA or deployment.
- **Comparison coverage.** Both endpoints require every expected server's successful CONNZ and known, uncapped connection totals. Missing, capped or unknown coverage produces an unavailable explanation. Groups absent on either side, unnamed groups and groups with any empty language, empty version or `0.0.0` version are counted as not compared. A new application or a rename does not prove a deployment; names can be reused within an account. Server process changes do not change this identity. Rollups read at most 20,000 grouped rows; reaching that limit suppresses the comparison with an explanation. The existing 200-step cap still applies. Database retention and the three-interval endpoint reach apply; archived data is not read.
- **Same-window context.** A library comparison includes Replay's system-wide reconnect-pair count and the warning/critical finding firings read between A and B. Unobserved reconnect counts remain unknown with a reason; capture gaps never become zero. Truncated finding reads are labelled. Up to eight finding links accompany the two collection links. Reconnect pairing follows `client-004`, including server shutdowns. This is association by the same bounded time window, with no application attribution or causal claim. Server upgrades, findings and Kubernetes observations retain their own story steps. Collection and event gaps can hide intervening activity. A collection link opens Applications filtered to the application and account at that observation time.

- **Replay this window.** The Overview lists each observed Kubernetes window below its timeline with a normal link to Replay. A and B use the observed start and end widened by five minutes on either side. Existing lane links, Each check, table controls and the zoom strip remain available. The desktop finding page offers context around its latest recorded transition within the selected range, or its first sighting or resolution when those are the latest known times in that range. It reads the newest 100 transitions across all time; a newer episode outside the selected range is ignored. Condition start and the finding's lifetime are not treated as an episode boundary. The link names the observation and explicitly says the context is not the finding's duration. For an unresolved finding, B is surrounding context, not a claimed resolve. Without known timing in the selected range there is an explanation instead of a link. On phones, the existing episode story's Replay link is labelled "Replay this window" and fixes its supplied story interval; the phone card continues to show the current episode, while All details offers the selected-range context.
- **Entry link limits.** Each entry writes explicit A, B and a fixed range from A to B, even when the source is live. Context can extend beyond the source range; B never extends beyond now. The existing one-collection-interval minimum and 24-hour maximum apply, with adjustment text beside the link. Times use UTC to the second. Links preserve the system; Replay is system-wide and drops the account scope under the shared navigation policy. Unrelated page filters are dropped. The context is an observation window, not evidence of causation, a known incident duration or complete capture. Old observations are not silently shifted into retention or stored history: Replay still explains missing collections and history, and refuses A older than database retention. Opening a fixed link later does not make missing evidence available.

- **Shared gap rules.** Replay and [Data history](data-history.md) identify collection gaps longer than three collect intervals, recorded collector downtime, Kubernetes-watch gaps, and event-connection losses using shared rules. Event losses shorter than 10 seconds do not count as capture gaps. The pages present gaps differently: Data history adds the before-history span and merges overlapping collection losses for chart hatching; Replay keeps its story and observation reasons. The explanation when A predates history links to Data history with the global query.

- **Two moments.** A and B are in the address as `?a=` and `?b=` (UTC, to the second), beside the page's range and system. A link without them opens on the newest Kubernetes window of the range, widened by five minutes on each side, else on the last hour of the range; nats-lens writes the moments it chose into the address. A must be before B, at least one collect interval and at most 24 hours apart, and B is never after now. When a link breaks a rule, nats-lens corrects it and says what it changed, such as "A was after B, so the two were swapped." or "Replay reads at most 24 hours, so A was moved to 24 hours before B." A moment outside the page's range still loads, and the page says so ("A is before this range."). An A before the history starts gets the note that nats-lens stored nothing before then.
- **The strip.** The scrubber draws the worst status of each bucket of the range, from the same timeline as the Overview, with buckets without data dashed. Under it each Kubernetes window, such as a node upgrade, is a button that sets A and B to the window widened by five minutes on each side ("Replay GKE node upgrade 09:16–09:44 UTC"). The two handles A and B move by drag, by a click on the track (the nearer handle moves), or by keyboard: an arrow moves one bucket, Shift with an arrow ten, and Home and End go to the ends. Two inputs, "A (UTC)" and "B (UTC)", take a typed moment on Enter or on leaving the field. The address changes when a handle is released. The story loads once A and B have rested for 300 ms; while it loads, the earlier story stays dimmed under "Loading …; below is the earlier choice, …".
- **The story.** "What happened, in order" lists the steps from A to B, both ends included, in time order. Each step has its time or its span, a dot, a title, a sentence or two, and its evidence. A span that began before A keeps its real start, such as a restart whose old process last answered just before A. A time on another day than A carries its day, such as "14 Oct 23:00:00"; so does a Kubernetes window's time under the strip. The kinds of step:
  - **Kubernetes:** a node drained or upgraded, and a NATS pod that cannot be scheduled, backs off or is killed for memory, with the window it lies in ("Within the GKE node upgrade window, 08:44–09:20.").
  - **Restart:** a server whose process was replaced, as a span from the old process's last answer to the new one's first. The body says whether a lame duck or shutdown announcement came first ("Lame duck mode announced at 08:44:06."). It says "No lame duck or shutdown event was captured." only when events were captured over the whole time an announcement could come. It gives how many streams and consumers the server held when it came back, compared with before ("2 consumers fewer than before the restart"), and where the leaders it held were elected ("Leaders it held were elected on other servers: 2 on nats-us-0."). A restart to another version is titled "… restarts for an upgrade from … to …". The pod's Killing and started events and the `server-004` finding are evidence of the step, not steps of their own.
  - **Stop:** a server that answered in the collection at A or later and not in the one at B ("nats-us-4 stops answering"), with its announcement and the leaders it held. A stop is a restart step with warning severity, as is every restart.
  - **Leaders:** a meta leader change ("Meta leader moves from nats-1 to nats-0") and the leader elections that no restart or stop explains, folded by the new leader. An election that keeps the leader makes no step.
  - **Quorum:** a quorum loss of `stream-001`, `consumer-001` or `system-002` as one span from its firing to its resolve ("Stream inventory-changes has no quorum for 2 min"), with the quorum advisories as evidence. A loss already open at A starts at the firing that began it; a loss not resolved by B ends "still at B". Losses of one check that start close together fold into one step across assets.
  - **Gone:** a stream or durable consumer that servers held at A and no server reports at B (see below).
  - **Finding:** another warning or critical finding firing or changing severity, folded by check. Info findings are only evidence.
  - **Resolved:** a finding resolving, with the ok colour.
  - **Gap:** a time nats-lens knows nothing of, drawn dashed: no collection for more than three collect intervals ("No collections since 11:29:56, still at B"), nats-lens not running (`collector.gap`), a gap in the Kubernetes watch (`collector.k8s_gap`), or a time a connection that captures events was lost ("Events not captured since 11:30:00, still at B": from its disconnect, a time without a server or its close until it reconnects or nats-lens starts again). Gaps at the start and the end of A to B count too.
- **Folding.** Steps of one kind about the same thing (one check, one server, or one Kubernetes kind on one server) within 2 minutes or two collect intervals, whichever is longer, become one step with a count, such as "The pod of nats-us-4 cannot be scheduled (5 times, 09:12–09:18)".
- **Order alone.** No step claims a cause; two steps next to each other are only next to each other in time. Every step except a gap names the event, finding or collection it was built from.
- **After B.** Below an "After B" heading come the steps that close a step of the story, each tagged "outside the range": the first resolve of a warning or critical finding that has a step, and a stopped server answering again. nats-lens looks up to 6 hours past B, never past now.
- **Between A and B.** Tiles count servers restarted, servers stopped (not back by B), streams and consumers gone without a delete, assets gone with a delete, assets unknown at B, leaders moved (of how many elections), meta leader changes, quorum lost advisories ("NATS advisories, not server logs"), node pools upgraded (with the number of nodes), nodes drained, client reconnects and findings raised (warning and critical). A zero tile is drawn quieter. Without node pool labels the tile reads "Nodes upgraded". With no node upgraded between A and B, the node pool count is 0 and observed, not "no node pool labels". A leader move is an election whose new leader differs from the asset's previous one; an election whose previous leader is unknown is never a move. Client reconnects pair each client name's disconnects with its connects, as `client-004` does, and leave tooling connections out.
- **Counts not observed.** nats-lens never shows a count it could not observe as 0. The page names such counts below the tiles, grouped by the reason nats-lens gives for each, with the times of the gap or cut behind it, for example "Not observed (nats-lens was not running 09:40:00–still at B): leaders moved and client reconnects." The reasons: no collection between A and B, no collection at A, no collection at B, no event stored up to B, nats-lens not running, an events connection lost, the read limit reached, Kubernetes not watched for the system, a time Kubernetes was not watched, a node event that names no node, no node pool label, and gone assets not read. A count without a known reason is named in a plain "Not observed: …" line.

  Restarts and findings raised need a collection between A and B; stops also need the collection at B, and the gone counts the collections at both A and B. The leader, quorum and reconnect counts need stored events up to B, nats-lens running and its events connections connected between A and B, and (all but reconnects) a read that reached B; a time without collections does not unset them. An events connection that reconnects within 10 seconds failed over: that is no loss of capture, so no gap step and no unset count, though advisories sent in those seconds may be missing. The loss of any one events connection for longer unsets them all, though each connection carries only some of the events (reconnects come on the system account's): the page errs towards "not observed". The collection at A or B is the newest one that started at or before it, within three collect intervals. Without one the page says "No collection at A: none started in the three collect intervals before it, so the counts that need it are not observed."
- **Gone between A and B.** A table lists each stream (KV buckets and object stores included) and each durable consumer that a server reported in the collection at A and none in the collection at B. Consumers follow the rule of `consumer-013`: durable, without an inactive threshold, and not a `JS_` internal one; a gone stream's durables count with it ("Durables"). Each row gives:
  - the kind, and the servers that held it at A ("3 servers: nats-us-0, nats-us-1, nats-us-2");
  - at B, "none", or "none answering: nats-us-4" when a holder did not answer at B;
  - the delete event, linked by its summary, which names the client that sent it ("analytics-aggregator on order-archive by analytics-worker@10.20.0.12"); "by analytics-worker (10.20.0.12)" is added only for an event without a summary; or "none captured";
  - when it was last reported;
  - under "Durables", how many durables went with a stream, and a dash for a consumer.

  The asset's name opens its page as it was at A. "Compare in Placement" opens Placement at B compared with A ([placement.md](placement.md)); it is offered only when the gone assets were observed. When they were not, the section says "Not observed for this A and B" and "Between A and B" gives the reason. An asset whose holder did not answer at B counts as unknown at B, not as gone without a delete. In the story, an asset gone without a delete event is critical; one gone with a delete, or with a holder not answering, is a warning. Its step names its findings of `consumer-013`, `stream-014`, `consumer-010` and `stream-011`.
- **Evidence links.** An event opens on the Events page with that event on top ([events.md](events.md)); a finding opens its page; a collection opens its server's, stream's or consumer's page, or Placement for the whole system, over the hour that ends a second after it, so the page shows the state at that collection. A step lists at most 10 pieces of evidence; when it has more, it lists its own evidence first and fills the rest with elections, in time order. Two or more leader elections of a step on one new leader fold into one line, such as "11 leader elections on nats-1 (6 of 11 listed)", that opens to the links of the listed ones; the count covers all the step's elections, also those past the 10, and a leader whose elections are all past them gets a line of its own ("3 leader elections on nats-3, none listed"). A meta leader step is not folded, so its elections keep their order. "and N more" then counts only the other evidence left out. The incident log folds the same way.
- **Copy link to A–B.** The button copies the absolute link to this A and B. A live range becomes a fixed range ending now, on the page too, so whoever opens the link sees the same strip: "Link to A–B copied; the range is now fixed, so the link shows the same strip." When the browser refuses the clipboard, the status line gives the link to copy by hand.
- **Export as incident log.** The button downloads the story as plain text, `replay-<system>-<A>-<B>.txt`: a header with the system, A and B in UTC, the collections at A and B and the link; "What happened, in order", one line per step (time, kind, severity, title) with its body and its evidence as absolute links; "After B"; "Between A and B", one line per count ("not observed" with its reason for an unset one); "Gone between A and B"; and the gaps.
- **Limits.** At most 200 steps between A and B and 10 after B. When more happened, the page says how many were left out; critical, warning, restart, gone and gap steps are kept first. At most 50 entities per step and 200 gone assets ("More assets went than the 200 listed."). nats-lens reads at most 20,000 events and 20,000 finding transitions, the earliest first. When the limit cuts them, the page says "Steps after … may be missing: the read limit was reached.", and the API gives `complete_until`, the time up to which both were read in full. The counts taken from the cut rows are then not observed, unless the read reached past B.
- **Retention.** Replay reads the database, not the archive. nats-lens refuses an A older than the retention: "invalid argument: A is older than retention (14d); Replay reads the database only".
- **System switch.** The story belongs to the system in the address. After a change of system the page asks for the new system's story at once, and never shows another system's story as the earlier choice.
- **The API.** `GetReplay` takes a system, A and B. It refuses an A not before B, and A and B more than 24 hours apart, as an invalid argument, and reads a B after now as now. It returns:
  - the steps in order, each with its kind, severity, time and end, whether its span was still open at B, whether it is outside the range, a gap's kind, its entities, its evidence (an election's names the server it elected, `elected`) and all its leader elections counted by new leader (`leaders_taken`, most first, not capped);
  - the counts, unset when not observed, and the gone assets;
  - the Kubernetes windows, and whether Kubernetes is watched;
  - the collections at A and B, and when the history starts.
- **The demo's story.** In `natslens demo`, `example-global`'s `us` node pool upgrade (08:44 to 09:20 in the scenario's frame) is the story. `nats-us-1` to `nats-us-3` announce lame duck mode and restart, and their leaders are elected elsewhere. Two durables on `inventory-changes` are gone without a delete event, and the stream loses quorum for two minutes. `nats-us-4` stops answering and takes `KV_label-templates` with it. `example-prod`'s upgrade has a consumer deleted by a client, and `example-staging` a time without collections ([demo-mode.md](demo-mode.md)).

## Scenarios

### Compare observed client-library sets

- **Surfaces:** `ui`, `api`
- **Given** complete CONNZ collections near A and B with the same named application and account, and Go 1.0 at A and Go 1.0 plus Go 1.1 at B
- **When** I open Replay for A to B
- **Then** a client-library comparison shows both mixes with connection counts and links to both Applications observations
- **And** reconnects and finding firings are explicitly system-wide same-window context, without a deployment or causal claim

### Keep missing client observations unknown

- **Surfaces:** `ui`, `api`
- **Given** capped CONNZ at B, or an application carrying version `0.0.0`
- **When** I open Replay
- **Then** Replay explains unavailable coverage or counts that group as not compared, rather than inventing a deployment or a zero version mix


### Replay an observed window without losing Overview controls

- **Surfaces:** `ui`
- **Given** an Overview timeline with two observed Kubernetes windows
- **When** I follow either "Replay this window" link, including with a keyboard or a modified click
- **Then** its normal link opens Replay for that window plus five minutes on each side, within the existing limits, with explicit A and B and a fixed range on the same system
- **And** the Overview's lane links, Each check, table controls and zoom strip retain their interactions

### Replay a historical finding observation

- **Surfaces:** `ui`
- **Given** a finding with a newer episode and recorded transitions inside a selected historical range
- **When** I follow "Replay this window" in its desktop details
- **Then** Replay opens a fixed context window around the newest recorded transition in that range, ignoring the newer episode and condition start
- **And** a resolution is labelled as a resolution; an unresolved finding's context end is never described as its resolve

### Missing timing and missing evidence

- **Surfaces:** `ui`
- **Given** no recorded transition, first sighting or resolution time in the selected range
- **When** I read the finding's details
- **Then** an explanation replaces the Replay link, including the 100-transition history limit
- **And** when timing is known but retained observations are missing, the link keeps that timing and Replay retains its history, collection and retention explanations


### Explain a node pool upgrade

- **Surfaces:** `ui`
- **Given** the demo's `example-global`, whose `us` node pool was upgraded between 08:44 and 09:20
- **When** I open Replay and choose the window's button under the strip
- **Then** A and B are set to 08:39 and 09:25, the window widened by five minutes on each side, and the story lists, in order with their times, each node drained and upgraded, the restarts of `nats-us-1`, `nats-us-2` and `nats-us-3`, the quorum loss of `inventory-changes`, the two gone durables and `nats-us-4` stopping
- **And** "Between A and B" shows 3 servers restarted, 1 stopped, 1 node pool upgraded (3 nodes), 4 nodes drained and 150 client reconnects

### Read a restart with its announcement

- **Surfaces:** `ui`
- **Given** `nats-us-1` announced lame duck mode at 08:44:06, after its pod was killed, and came back at 08:46:26
- **When** I open the Replay of the upgrade window
- **Then** the step "nats-us-1 restarts" spans 08:43:56 to 08:46:26 and says "Lame duck mode announced at 08:44:06. Back at 08:46:26 with 1 stream and 4 consumers, as before the restart."
- **And** it says where the leaders it held were elected, and links the pod events, the announcement, the elections, the `server-004` finding and the collections before and after

### Read a restart without an announcement

- **Surfaces:** `ui`
- **Given** the demo's `example-prod`, whose `nats-0` restarted at 09:16 without a lame duck event
- **When** I set A to 09:14 and B to 09:50
- **Then** the step "nats-0 restarts" says "No lame duck or shutdown event was captured."

### See a server stop

- **Surfaces:** `ui`
- **Given** `nats-us-4` announced its shutdown at 09:12:05 and did not answer again by B
- **When** I open the Replay of the upgrade window
- **Then** the step "nats-us-4 stops answering" runs from 09:11:56 to "still at B" and says "Shutdown announced at 09:12:05. Last answered at 09:11:56; not answering at B."

### See a quorum loss as one span

- **Surfaces:** `ui`
- **Given** `stream-001` fired for `inventory-changes` at 08:58:40 and resolved at 09:01:00
- **When** I open the Replay of the upgrade window
- **Then** one step "Stream inventory-changes has no quorum for 2 min" spans 08:58:40 to 09:01:00, with the advisory and both transitions as evidence

### Find assets that vanished

- **Surfaces:** `ui`
- **Given** two durables on `inventory-changes` that `nats-us-0`, `nats-us-1` and `nats-us-2` held at A and no server holds at B, without a delete event
- **When** I open the Replay of the upgrade window
- **Then** "Gone between A and B" lists `restock-alerts` and `stock-audit-export` with "3 servers" at A, "none" at B and "none captured" for the delete event
- **And** the story has the critical step "2 durables on inventory-changes gone without a delete event", and the count of consumers gone without a delete is 2

### Tell a deleted asset from a vanished one

- **Surfaces:** `ui`
- **Given** the demo's `example-prod`, where `analytics-worker` deleted the durable `analytics-aggregator` at 09:31:07
- **When** I set A to 09:14 and B to 09:50
- **Then** the gone table names the delete event by its summary, which names `analytics-worker`, the story has the warning step "Durable analytics-aggregator on order-archive deleted", and the count of assets gone with a delete is 1

### Know when a holder did not answer at B

- **Surfaces:** `ui`
- **Given** `KV_label-templates` was held only by `nats-us-4`, which stopped answering
- **When** I open the Replay of the upgrade window
- **Then** its row says "none answering: nats-us-4" at B, the story says "Stream KV_label-templates not reported at B", and it counts as an asset unknown at B, not as gone without a delete

### See what closed a step after B

- **Surfaces:** `ui`
- **Given** the `consumer-013` findings of the two gone durables resolved at 09:58:56, after B
- **When** I open the Replay of the upgrade window
- **Then** below "After B" the step "Durable consumer disappeared resolved for 2 consumers" is tagged "outside the range"

### See a time without collections

- **Surfaces:** `ui`
- **Given** the demo's `example-staging`, whose collections ended at 11:29:56
- **When** I set A to 11:00 and B to 12:30
- **Then** a dashed gap step says "No collections since 11:29:56, still at B", and the page says "No collection at B"
- **And** the servers restarted and findings raised tiles show 0, and the stopped and gone counts are named as not observed with the reason nats-lens gives, never as 0

### Refuse a moment older than the retention

- **Surfaces:** `api`
- **Given** a retention of 14 days
- **When** a client calls `GetReplay` with an A older than that
- **Then** it receives `invalid_argument` with "A is older than retention (14d); Replay reads the database only"

### Share the moments

- **Surfaces:** `ui`
- **Given** Replay on a live range with A and B chosen
- **When** I choose "Copy link to A–B"
- **Then** the range becomes fixed, the absolute link with `?a=` and `?b=` is on the clipboard, and the page says "Link to A–B copied; the range is now fixed, so the link shows the same strip."

### Export the incident log

- **Surfaces:** `ui`
- **Given** the story of the upgrade window has loaded
- **When** I choose "Export as incident log"
- **Then** a text file `replay-example-global-<A>-<B>.txt` downloads with the steps in order, each with its evidence as absolute links, then "After B", the counts, the gone assets and the gaps

### Switch the system

- **Surfaces:** `ui`
- **Given** Replay of `example-global` shows its story
- **When** I switch to `example-prod`
- **Then** the page asks for `example-prod`'s story at once and does not show `example-global`'s as the earlier choice

### Fetch the story from the API

- **Surfaces:** `api`
- **Given** two moments within the retention
- **When** a client calls `GetReplay` with them
- **Then** it receives the ordered steps with links to their evidence, the counts with the unobserved ones unset, and the gone assets

## Not supported / out of scope

- **No moments older than the retention.** Replay reads the database, not the Parquet archive ([history-and-archive.md](history-and-archive.md)).
- **No causes.** Replay does not say why a server restarted beyond the Kubernetes event's own note, nor that one step caused another.
- **No "from disk".** A restart step says how many streams and consumers a server held when it came back, not where it recovered them from.
- **No quorum reports from server logs.** The quorum count is of NATS advisories; nats-lens does not read server logs.
- **No more than 24 hours** between A and B.
- **No step for replicas catching up.** A lagging replica shows only as its `stream-003` or `server-012` finding.
- **No leader moves without advisories.** nats-lens counts leaders moved from election advisories only, not from differences between collections.
- **No reconnects per application.** Client reconnects are one count; the page does not say which applications reconnected. The [Applications](applications.md) page counts each application's reconnects over the 24 hours before a collection.
- **No events gap in the first 30 seconds after a start.** When nats-lens starts and an events connection does not connect, nothing marks the time until `collector.not_connected` is recorded 30 seconds later; those seconds count as captured.
- **Text export only.** The incident log is plain text; there is no Markdown or JSON export.

// Events: the events of the range, newest first. It opens on what is
// worth reading (routine events hidden, repeats folded), with a histogram,
// counts per source and server, and new events held behind a pill. ?id=
// opens one event on top.
import { useEffect, useState, type FormEvent } from "react";
import { useSearchParams } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useEventFeed, useNewEvents, type EventFeed, type FeedFilter } from "../api/events";
import type { Entity } from "../api/model";
import { useEvent } from "../api/queries";
import { Empty, ErrorBox, QueryView } from "../components/common";
import { usePager } from "../components/Pager";
import { EventFacets, SOURCES } from "../components/EventFacets";
import { EventHistogram } from "../components/EventHistogram";
import { EventRow } from "../components/EventRow";
import { fmtInt, upperFirst } from "../lib/format";
import { usePageTitle } from "../lib/usePageTitle";
import { useSystem } from "../lib/useSystem";
import { rangeKey, rangeLabel, type RangeSpec } from "../lib/timeRange";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";
import { entityKeyLabel, withAccountName } from "../lib/view";

const empty = { account: "", stream: "", consumer: "", server: "", name: "" };

/** The entity filter from ?stream=A/s, ?consumer=A/s/c or ?server=n. */
export function entityFromParams(p: URLSearchParams): Entity | undefined {
  const c = p.get("consumer");
  if (c) {
    const [account = "", stream = "", consumer = ""] = c.split("/");
    return { ...empty, kind: "consumer", key: c, account, stream, consumer, name: consumer };
  }
  const s = p.get("stream");
  if (s) {
    const [account = "", stream = ""] = s.split("/");
    return { ...empty, kind: "stream", key: s, account, stream, name: stream };
  }
  const v = p.get("server");
  if (v) return { ...empty, kind: "server", key: v, server: v, name: v };
  return undefined;
}

function OneEvent({ id, end }: { id: string; end: number }) {
  const [params]=useSearchParams();
  const q = useEvent(id,params.get("eventReadId")??undefined);
  const [open, setOpen] = useState(true);
  if (q.error) return <ErrorBox error={q.error} what={`event ${id}`} retry={() => q.refetch()} />;
  if (!q.data) return null;
  return (
    <section className="sec" aria-label={`Event ${id}`}>
      <div className="card evlist">
        <EventRow e={q.data} end={end} open={open} onToggle={() => setOpen((o) => !o)} />
      </div>
    </section>
  );
}

/** "Hidden as routine: 482 events · <group> ×448 · …", with Show them. */
function RoutineLine({ feed, onShow }: { feed: EventFeed; onShow: () => void }) {
  if (feed.routineHidden <= 0) return null;
  return (
    <section className="ev-routine" aria-label="Hidden as routine">
      <span>
        Hidden as routine: <b className="mono">{fmtInt(feed.routineHidden)}</b> events
        {feed.routineGroups.map((g) => (
          <span key={`${g.kind}|${g.clientName}`}>
            {" · "}
            <span>{g.label}</span> <span className="mono">×{fmtInt(g.count)}</span>
          </span>
        ))}
      </span>
      <button type="button" className="linkish" onClick={onShow}>
        Show them
      </button>
    </section>
  );
}

/** The counts are on the toggle above; the pager names only the page. */
function FeedPager({ page, feed, onNext, onPrev }: { page: number; feed: EventFeed; onNext: () => void; onPrev: () => void }) {
  if (page <= 1 && !feed.nextPageToken) return null;
  return (
    <nav className="pager" aria-label="Pagination">
      <span>page {page}</span>
      <span className="btns">
        <button type="button" className="btn" onClick={onPrev} disabled={page <= 1}>
          Newer
        </button>
        <button type="button" className="btn" onClick={onNext} disabled={!feed.nextPageToken}>
          Older
        </button>
      </span>
    </nav>
  );
}

/** Describe only what the feed establishes, never infer a client's capture gap. */
function EmptyFeed({ feed, filter, spec, system, page, clearClient, showRoutine }: {
  feed: EventFeed; filter: FeedFilter; spec: RangeSpec; system: string; page: number;
  clearClient: () => void; showRoutine: () => void;
}) {
  const hidden = filter.worthReading && feed.routineHidden > 0;
  const title = page > 1 ? "No more events on this page" : hidden ? "Matching events are hidden as routine"
    : feed.totalUnfiltered === 0 ? "No events captured in this range" : "No events match these filters";
  const range = spec.kind === "live" ? rangeLabel(spec) : `${new Date(feed.start).toISOString()} to ${new Date(feed.end).toISOString()}`;
  return (
    <section aria-label={title}>
      <Empty>
        <div>
          <h2>{title}</h2>
          <p>System: <code>{system || "default"}</code>. Range: {range}.</p>
          {filter.clientName && <p>Client: <code>{filter.clientName}</code>.</p>}
          {(filter.sources.length > 0 || filter.kinds.length > 0 || filter.entity) && (
            <p>Filters: {[filter.sources.join(", "), filter.kinds.join(", "), filter.entity && `${filter.entity.kind}: ${entityKeyLabel(filter.entity)}`].filter(Boolean).join(" · ")}.</p>
          )}
          {page > 1 ? <p>Use Newer to return to the preceding page.</p> : hidden ? (
            <p>{fmtInt(feed.routineHidden)} matching events are captured in this range, but hidden in Worth reading.</p>
          ) : (
            <>
              <p>{feed.totalUnfiltered === 0 ? "This system has no captured events in the selected range." : "Captured events exist in this range, but none match the active filters, including routine events."} Try a wider range using the time range control, or adjust the filters below.</p>
              {filter.clientName && <p>Connections shows a connection snapshot; Events shows captured event history. A connection may have opened before capture began, or its events may fall outside this range. An empty history does not mean the client does not exist or that nothing happened.</p>}
            </>
          )}
          <div className="btns">
            {hidden && page === 1 && <button type="button" className="btn p" onClick={showRoutine}>Show routine events</button>}
            {filter.clientName && <button type="button" className="btn" onClick={clearClient}>Clear client filter</button>}
          </div>
        </div>
      </Empty>
    </section>
  );
}

export function Events() {
  usePageTitle("Events");
  const [params, setParams] = useSearchParams();
  const sys = useSystem();
  const qc = useQueryClient();
  const { spec } = useTimeRange();
  const { end } = useResolvedRange(spec);
  const srcIds = (params.get("src") ?? "").split(",").filter(Boolean);
  const sources = SOURCES.filter((s) => srcIds.includes(s.id)).map((s) => s.source);
  const kinds = (params.get("kind") ?? "").split(",").map((k) => k.trim()).filter(Boolean);
  const fromParams = entityFromParams(params);
  const client = params.get("client") ?? "";
  const worthReading = params.get("show") !== "all";
  const id = params.get("id") ?? undefined;
  const pager = usePager([sys, sources.join(), kinds.join(), fromParams?.key, client, worthReading, rangeKey(spec)].join("|"));
  const filter: FeedFilter = { eventReadId:params.get("eventReadId")??undefined, evidenceRange:params.has("eventReadId")&&params.has("from")&&params.has("to")?{start:params.get("from")!,end:params.get("to")!}:undefined, sources, kinds, entity: fromParams, clientName: client, worthReading, pageSize: 50, pageToken: pager.token };
  const q = useEventFeed(spec, filter);
  // Placeholder results belong to a previous filter. A failed empty refresh
  // cannot establish an empty result either; show the error and Retry instead.
  const feed = !q.isPlaceholderData && !(q.error && q.data?.items.length === 0) ? q.data : undefined;
  const fresh = useNewEvents(spec, filter, q.data && !q.isPlaceholderData ? q.data : undefined);
  // The URL holds the account ID; the loaded events name it.
  const entity = fromParams && withAccountName(fromParams, q.data?.items ?? []);
  const [open, setOpen] = useState<Set<string>>(new Set());
  const [kindText, setKindText] = useState(kinds.join(", "));
  const [clientText, setClientText] = useState(client);

  useEffect(() => setClientText(client), [client]);
  const kindsKey = kinds.join(", ");
  useEffect(() => setKindText(kindsKey), [kindsKey]);

  const setParam = (k: string, v: string) =>
    setParams((p) => {
      const n = new URLSearchParams(p);
      if (v) n.set(k, v);
      else n.delete(k);
      return n;
    });
  const clearClient = () => {
    setClientText("");
    setParam("client", "");
  };
  const showRoutine = () => setParam("show", "all");
  const toggleSource = (sid: string) => {
    const next = srcIds.includes(sid) ? srcIds.filter((x) => x !== sid) : [...srcIds, sid];
    setParam("src", next.join(","));
  };
  const setServer = (name: string) =>
    setParams((p) => {
      const n = new URLSearchParams(p);
      ["stream", "consumer", "server"].forEach((k) => n.delete(k));
      if (name) n.set("server", name);
      return n;
    });
  const applyText = (e: FormEvent) => {
    e.preventDefault();
    setParams((p) => {
      const n = new URLSearchParams(p);
      if (kindText.trim()) n.set("kind", kindText.replace(/\s+/g, ""));
      else n.delete("kind");
      if (clientText.trim()) n.set("client", clientText.trim());
      else n.delete("client");
      return n;
    });
  };
  const toggle = (eid: string) =>
    setOpen((s) => {
      const n = new Set(s);
      if (n.has(eid)) n.delete(eid);
      else n.add(eid);
      return n;
    });
  const showNew = () => qc.invalidateQueries({ queryKey: ["eventFeed", sys] });

  return (
    <>
      <div className="page-h">
        <span className="aside">NATS advisories, Kubernetes events, finding transitions and the collector's own records, newest first.</span>
      </div>
      {id && <OneEvent id={id} end={end} />}
      {feed && feed.items.length === 0 && <EmptyFeed feed={feed} filter={filter} spec={spec} system={sys} page={pager.page} clearClient={clearClient} showRoutine={showRoutine} />}
      {feed && feed.histogram.some((b) => b.warning + b.notable + b.routine > 0) && <EventHistogram buckets={feed.histogram} start={feed.start} end={feed.end} live={spec.kind === "live"} worthReading={worthReading} />}
      {feed && (
        <EventFacets
          feed={feed}
          worthReading={worthReading}
          setWorthReading={(on) => setParam("show", on ? "" : "all")}
          srcIds={srcIds}
          toggleSource={toggleSource}
          clearSources={() => setParam("src", "")}
          server={fromParams?.kind === "server" ? fromParams.server : ""}
          setServer={setServer}
          showServers={!fromParams || fromParams.kind === "server"}
        />
      )}
      <div className="filters">
        <form className="grp wide" onSubmit={applyText} role="search" aria-label="Filter events">
          <input type="search" aria-label="Kinds, comma separated" placeholder="kinds, e.g. consumer.deleted" value={kindText} onChange={(e) => setKindText(e.target.value)} />
          <input type="search" aria-label="Client connection name" placeholder="client name" value={clientText} onChange={(e) => setClientText(e.target.value)} />
          <button type="submit" className="btn">
            Filter
          </button>
        </form>
        {entity && entity.kind !== "server" && (
          <span className="grp">
            <span className="lbl">{upperFirst(entity.kind)}</span>
            <span className="mono" title={entity.key}>{entityKeyLabel(entity)}</span>
            <button type="button" className="chip" onClick={() => setServer("")}>
              Clear
            </button>
          </span>
        )}
      </div>
      {feed && worthReading && <RoutineLine feed={feed} onShow={showRoutine} />}
      {/* Mounted always, so a screen reader hears the pill when it appears. */}
      <div className="ev-new" role="status" aria-live="polite">
        {fresh > 0 && (
          <button type="button" className="btn p" onClick={showNew} aria-label={`${fmtInt(fresh)} new events, show`}>
            {fmtInt(fresh)} new
          </button>
        )}
      </div>
      <QueryView q={{ ...q, data: feed }} what="events">
        {(page) => (
          <div className={page.items.length || pager.page > 1 || page.nextPageToken ? "card evlist" : undefined}>
            {page.items.map((e) => <EventRow key={e.id} e={e} end={end} open={open.has(e.id)} onToggle={() => toggle(e.id)} />)}
            <FeedPager page={pager.page} feed={page} onNext={() => pager.next(page.nextPageToken)} onPrev={pager.prev} />
          </div>
        )}
      </QueryView>
    </>
  );
}

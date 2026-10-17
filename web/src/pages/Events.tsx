// Events: the events of the range, newest first. It opens on what is
// worth reading (routine events hidden, repeats folded), with a histogram,
// counts per source and server, and new events held behind a pill. ?id=
// opens one event on top.
import { useState, type FormEvent } from "react";
import { useSearchParams } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useEventFeed, useNewEvents, type EventFeed, type FeedFilter } from "../api/events";
import type { Entity } from "../api/model";
import { useEvent } from "../api/queries";
import { Empty, ErrorBox, QueryView } from "../components/common";
import { usePager } from "../components/EntityParts";
import { EventFacets, SOURCES } from "../components/EventFacets";
import { EventHistogram } from "../components/EventHistogram";
import { EventRow } from "../components/EventRow";
import { fmtInt } from "../lib/format";
import { usePageTitle } from "../lib/usePageTitle";
import { useSystem } from "../lib/useSystem";
import { rangeKey } from "../lib/timeRange";
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
  const q = useEvent(id);
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
  const filter: FeedFilter = { sources, kinds, entity: fromParams, clientName: client, worthReading, pageSize: 50, pageToken: pager.token };
  const q = useEventFeed(spec, filter);
  const fresh = useNewEvents(spec, filter, q.data && !q.isPlaceholderData ? q.data : undefined);
  // The URL holds the account ID; the loaded events name it.
  const entity = fromParams && withAccountName(fromParams, q.data?.items ?? []);
  const [open, setOpen] = useState<Set<string>>(new Set());
  const [kindText, setKindText] = useState(kinds.join(", "));
  const [clientText, setClientText] = useState(client);

  const setParam = (k: string, v: string) =>
    setParams((p) => {
      const n = new URLSearchParams(p);
      if (v) n.set(k, v);
      else n.delete(k);
      return n;
    });
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
      {q.data && <EventHistogram buckets={q.data.histogram} start={q.data.start} end={q.data.end} live={spec.kind === "live"} worthReading={worthReading} />}
      {q.data && (
        <EventFacets
          feed={q.data}
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
            <span className="lbl">{entity.kind}</span>
            <span className="mono" title={entity.key}>{entityKeyLabel(entity)}</span>
            <button type="button" className="chip" onClick={() => setServer("")}>
              Clear
            </button>
          </span>
        )}
      </div>
      {q.data && worthReading && <RoutineLine feed={q.data} onShow={() => setParam("show", "all")} />}
      {/* Mounted always, so a screen reader hears the pill when it appears. */}
      <div className="ev-new" role="status" aria-live="polite">
        {fresh > 0 && (
          <button type="button" className="btn p" onClick={showNew} aria-label={`${fmtInt(fresh)} new events, show`}>
            {fmtInt(fresh)} new
          </button>
        )}
      </div>
      <QueryView q={q} what="events">
        {(page) => (
          <div className="card evlist">
            {page.items.length === 0 ? (
              <Empty>No events match in this range.</Empty>
            ) : (
              page.items.map((e) => <EventRow key={e.id} e={e} end={end} open={open.has(e.id)} onToggle={() => toggle(e.id)} />)
            )}
            <FeedPager page={pager.page} feed={page} onNext={() => pager.next(page.nextPageToken)} onPrev={pager.prev} />
          </div>
        )}
      </QueryView>
    </>
  );
}

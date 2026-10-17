// Events: every event in the range, newest first, filtered by source, kind,
// entity and client, paged by time. ?id= opens one event on top.
import { useState, type FormEvent } from "react";
import { useSearchParams } from "react-router";
import type { Entity, EventSourceName } from "../api/model";
import { useEvent, useEvents } from "../api/queries";
import { Empty, ErrorBox, QueryView } from "../components/common";
import { Pager, usePager } from "../components/EntityParts";
import { EventRow } from "../components/EventRow";
import { usePageTitle } from "../lib/usePageTitle";
import { rangeKey } from "../lib/timeRange";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";
import { entityKeyLabel, withAccountName } from "../lib/view";

const SOURCES: { id: string; label: string; source: EventSourceName }[] = [
  { id: "nats", label: "NATS", source: "nats" },
  { id: "kubernetes", label: "Kubernetes", source: "kubernetes" },
  { id: "findings", label: "Findings", source: "finding" },
  { id: "collector", label: "Collector", source: "collector" },
];

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

export function Events() {
  usePageTitle("Events");
  const [params, setParams] = useSearchParams();
  const { spec } = useTimeRange();
  const { end } = useResolvedRange(spec);
  const srcIds = (params.get("src") ?? "").split(",").filter(Boolean);
  const sources = SOURCES.filter((s) => srcIds.includes(s.id)).map((s) => s.source);
  const kinds = (params.get("kind") ?? "").split(",").map((k) => k.trim()).filter(Boolean);
  const fromParams = entityFromParams(params);
  const client = params.get("client") ?? "";
  const id = params.get("id") ?? undefined;
  const pager = usePager([sources.join(), kinds.join(), fromParams?.key, client, rangeKey(spec)].join("|"));
  const q = useEvents(spec, { sources, kinds, entity: fromParams, clientName: client, pageSize: 50, pageToken: pager.token });
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

  return (
    <>
      <div className="page-h">
        <h1>Events</h1>
        <span className="aside">NATS advisories, Kubernetes events, finding transitions and the collector's own records, newest first.</span>
      </div>
      {id && <OneEvent id={id} end={end} />}
      <div className="filters">
        <div className="grp" role="group" aria-label="Sources">
          <span className="lbl">Source</span>
          <button type="button" className={`chip${srcIds.length === 0 ? " on" : ""}`} aria-pressed={srcIds.length === 0} onClick={() => setParam("src", "")}>
            All
          </button>
          {SOURCES.map((s) => (
            <button key={s.id} type="button" className={`chip${srcIds.includes(s.id) ? " on" : ""}`} aria-pressed={srcIds.includes(s.id)} onClick={() => toggleSource(s.id)}>
              {s.label}
            </button>
          ))}
        </div>
        <form className="grp wide" onSubmit={applyText} role="search" aria-label="Filter events">
          <input type="search" aria-label="Kinds, comma separated" placeholder="kinds, e.g. consumer.deleted" value={kindText} onChange={(e) => setKindText(e.target.value)} />
          <input type="search" aria-label="Client connection name" placeholder="client name" value={clientText} onChange={(e) => setClientText(e.target.value)} />
          <button type="submit" className="btn">
            Filter
          </button>
        </form>
        {entity && (
          <span className="grp">
            <span className="lbl">{entity.kind}</span>
            <span className="mono" title={entity.key}>{entityKeyLabel(entity)}</span>
            <button type="button" className="chip" onClick={() => setParams((p) => { const n = new URLSearchParams(p); ["stream", "consumer", "server"].forEach((k) => n.delete(k)); return n; })}>
              Clear
            </button>
          </span>
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
            <Pager
              page={pager.page}
              next={page.nextPageToken}
              onNext={() => pager.next(page.nextPageToken)}
              onPrev={pager.prev}
              total={page.totalCount}
              shown={page.items.length}
              newer="Newer"
              older="Older"
            />
          </div>
        )}
      </QueryView>
    </>
  );
}

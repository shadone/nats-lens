// The Overview's lower half: the event feed beside the streams and servers
// cards. At wide widths the feed takes the right column's height and
// scrolls inside, but never hides the expanded event.
import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import type { EventSourceName, LensEvent, Server, StreamSummary, StreamTotals, Window } from "../api/model";
import { MiniLine } from "../charts/MiniLine";
import { fmtBytes, fmtDuration, fmtGrowth, hm } from "../lib/format";
import { LEVEL_ORDER, consumerCount, serverPath, streamPath } from "../lib/view";
import { RLink, Num } from "./common";
import { EventRow } from "./EventRow";
import { HealthIcon } from "./Severity";

export const FEED_FILTERS: { id: string; label: string; sources: EventSourceName[] }[] = [
  { id: "all", label: "All", sources: [] },
  { id: "nats", label: "NATS", sources: ["nats"] },
  { id: "kubernetes", label: "Kubernetes", sources: ["kubernetes"] },
  { id: "findings", label: "Findings", sources: ["finding"] },
];

export function FeedChips({ value, onChange }: { value: string; onChange: (id: string) => void }) {
  return (
    <div className="right" role="group" aria-label="Event sources">
      {FEED_FILTERS.map((f) => (
        <button key={f.id} type="button" className={`chip${value === f.id ? " on" : ""}`} aria-pressed={value === f.id} onClick={() => onChange(f.id)}>
          {f.label}
        </button>
      ))}
    </div>
  );
}

interface FeedProps {
  events: LensEvent[];
  total: number;
  end: number;
  initialOpen?: string;
  moreHref: string;
  empty: ReactNode;
}

export function EventFeed({ events, total, end, initialOpen, moreHref, empty }: FeedProps) {
  const [open, setOpen] = useState<Set<string>>(() => new Set(initialOpen ? [initialOpen] : []));
  const wrap = useRef<HTMLDivElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const toggle = (id: string) =>
    setOpen((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });

  // Wide layout: grow the feed so the first expanded event stays in view.
  useLayoutEffect(() => {
    const w = wrap.current;
    const l = list.current;
    if (!w || !l) return;
    w.style.minHeight = "";
    const feed = w.firstElementChild as HTMLElement | null;
    const ex = l.querySelector<HTMLElement>(".ev.open");
    if (!feed || getComputedStyle(feed).position !== "absolute" || !ex) return;
    const more = feed.querySelector<HTMLElement>(".more");
    w.style.minHeight = `${ex.offsetTop + ex.offsetHeight + 12 + (more?.offsetHeight ?? 0)}px`;
  });

  const firstOpen = events.findIndex((e) => open.has(e.id));
  const older = firstOpen >= 0 ? events.length - firstOpen - 1 : 0;
  return (
    <div className="feed-wrap" ref={wrap}>
      <div className="card feed">
        <div className="list" ref={list} tabIndex={0} aria-label="Events, newest first">
          {events.length === 0 ? empty : events.map((e) => <EventRow key={e.id} e={e} end={end} open={open.has(e.id)} onToggle={() => toggle(e.id)} compact />)}
        </div>
        <RLink to={moreHref} className="more">
          All <Num v={total} /> events in range
          {older > 0 && <span className="older"> · {older} older in this list, scroll</span>}
        </RLink>
      </div>
    </div>
  );
}

export function StreamsCard({ streams, totals }: { streams: StreamSummary[]; totals: StreamTotals }) {
  const ss = [...streams].sort((a, b) => LEVEL_ORDER[a.status] - LEVEL_ORDER[b.status]);
  const expected = ss.some((s) => s.consumersExpected !== undefined);
  return (
    <div className="card st">
      <div className="st-h" aria-hidden="true">
        <span />
        <span>Stream</span>
        <span className="num">Messages</span>
        <span className="num">Bytes</span>
        <span className="num">Cons.</span>
        <span className="num">7 days</span>
        <span className="num">Per day</span>
      </div>
      {ss.map((s) => {
        const { text: cons, short } = consumerCount(s);
        const g7 = <MiniLine points={s.bytes7d?.points ?? []} level={s.status} label={`${s.entity.stream} bytes`} period="last 7 days" fmt={fmtBytes} />;
        return (
          <div className="st-r" key={s.entity.key}>
            <span>
              <HealthIcon level={s.status} />
            </span>
            <span className="nm">
              <RLink to={streamPath(s.entity.account, s.entity.stream)}>{s.entity.stream}</RLink>
              {s.findingCodes.length > 0 && <small>{s.findingCodes.join(", ")}</small>}
            </span>
            <span className="num msgs"><Num v={s.messages} /></span>
            <span className="num by"><Num v={s.bytes} fmt={fmtBytes} /></span>
            <span className={`num cons${short ? " short" : ""}`}>{cons}</span>
            <span className="gr7" style={{ justifySelf: "end" }}>
              {g7}
            </span>
            <span className="num gr"><Num v={s.bytesPerDay} fmt={fmtGrowth} /></span>
            <span className="g7m">{g7}</span>
            <span className="mline">
              <span>
                <em>msgs</em>
                <Num v={s.messages} />
              </span>
              <span>
                <em>size</em>
                <Num v={s.bytes} fmt={fmtBytes} />
              </span>
              <span className={short ? "short" : undefined}>
                <em>cons</em>
                {cons}
              </span>
              <span>
                <em>/day</em>
                <Num v={s.bytesPerDay} fmt={fmtGrowth} />
              </span>
            </span>
          </div>
        );
      })}
      <div className="st-foot">
        <span>Total <Num v={totals.bytes} fmt={fmtBytes} /> stored</span>
        <span>{totals.durableConsumers} durable consumers</span>
        {expected && <span>2/3 = present / expected by inventory</span>}
      </div>
    </div>
  );
}

export function serversAside(servers: Server[], windows: Window[]): string {
  if (!servers.length) return "No server reported in this range.";
  const up = servers.filter((s) => s.present).length;
  const versions = [...new Set(servers.map((s) => s.version))];
  const v = versions.length === 1 ? `all on v${versions[0]}` : `${versions.length} versions`;
  const restarts = servers.flatMap((s) => s.restarts);
  const win = windows.find((w) => restarts.length > 0 && restarts.every((t) => t >= w.start && t <= w.end));
  return `${up} of ${servers.length} up, ${v}.${win ? ` Restarted in the ${win.label} window.` : ""}`;
}

export function ServersCard({ servers }: { servers: Server[] }) {
  return (
    <div className="card st">
      <div className="sv-r h" aria-hidden="true">
        <span>Server</span>
        <span>Version</span>
        <span className="num">Uptime</span>
        <span className="role">Role</span>
        <span>Healthz</span>
      </div>
      {servers.map((s) => {
        const last = s.restarts[s.restarts.length - 1];
        const ok = s.present && (s.healthz === "ok" || s.healthz === "");
        return (
          <div className="sv-r" key={s.name}>
            <span className="nm">
              <RLink to={serverPath(s.name)}>{s.name}</RLink>
              {last !== undefined && <small>restarted {hm(last)}</small>}
            </span>
            <span>{s.version}</span>
            <span className="num">{s.uptimeMs !== undefined ? fmtDuration(s.uptimeMs) : "–"}</span>
            <span className="role">{s.role}</span>
            <span className={ok ? "ok" : "bad"}>{s.present ? s.healthz || "n/a" : "missing"}</span>
          </div>
        );
      })}
    </div>
  );
}

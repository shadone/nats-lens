// The Servers page's data points: who answers, versions in use, restarts,
// the JetStream store across servers and slow-consumer detections.
import {InfoTip} from "./MetricBox";
import { Link } from "react-router";
import { useGlobalQuery } from "../lib/useGlobalQuery";
import { slowEvidencePath } from "../lib/slowConsumers";
import type { ServerList, ServerRow } from "../api/servers";
import { SERVERS_QUERIES, SLOW_STRIP_QUERY, useServersTrends } from "../api/servers";
import { pickTrend } from "../api/trendBatch";
import { fmtInt, hm } from "../lib/format";
import { versionGroups } from "../lib/serverColour";
import { byName, median, storePct, STORE_ATTENTION_PCT } from "../lib/servers";
import type { RangeSpec } from "../lib/timeRange";
import { useReportStale } from "./StaleContext";

/** "24 h" for a live preset, "in range" for a fixed range. */
export function rangeShort(spec: RangeSpec): string {
  return spec.kind === "live" ? spec.preset.replace(/(\d+)/, "$1 ") : "in range";
}

function titled(label: string, spec: RangeSpec) {
  return spec.kind === "live" ? `${label}, ${rangeShort(spec)}` : `${label} in range`;
}

/** Up to three names, then how many more. */
function names(list: string[]): string {
  return list.length <= 3 ? list.join(", ") : `${list.slice(0, 3).join(", ")} and ${list.length - 3} more`;
}

function Answering({ list }: { list: ServerList }) {
  const seen = new Map(list.servers.map((s) => [s.name, s]));
  const all = [...new Set([...list.servers.map((s) => s.name), ...list.expected])].sort((a, b) => byName({ name: a }, { name: b }));
  const down = all.filter((n) => !seen.get(n)?.present);
  return (
    <div className="card dp">
      <span className="lbl">Answering <InfoTip label="Answering" text="Servers that answered the newest collection, out of observed and expected server names. A missing reply is not proof that the server process stopped."/></span>
      <div className="dp-v">
        <b className={down.length ? "bad" : undefined}>{all.length - down.length}</b> <em>of {all.length}</em>
      </div>
      <div className="dp-sq" role="img" aria-label={down.length ? `${down.length} did not answer: ${down.join(", ")}` : "every server answered"}>
        {all.map((n) => (
          <span key={n} className={`sq ${down.includes(n) ? "down" : "up"}`} title={`${n}: ${down.includes(n) ? "did not answer" : "answers"}`} />
        ))}
      </div>
      <div className="dp-n">
        {down.length ? (
          <>
            <span className="sq down" aria-hidden="true" /> {names(down)} did not answer
          </>
        ) : (
          "all answer"
        )}
      </div>
    </div>
  );
}

function Versions({ servers }: { servers: ServerRow[] }) {
  const groups = versionGroups(servers);
  const inUse = groups.reduce((a, g) => a + g.versions.length, 0);
  return (
    <div className="card dp">
      <span className="lbl">Versions <InfoTip label="Versions" text="NATS server versions in the latest server list. The coloured segments count servers, not releases or application-library versions."/></span>
      <div className="dp-v">
        <b>{inUse || "–"}</b> <em>in use</em>
      </div>
      <div className="dp-split" aria-hidden="true">
        {groups.map((g) => (
          <span key={g.cls} className={g.cls} style={{ flex: g.count }} />
        ))}
      </div>
      <ul className="dp-n dp-legend">
        {groups.map((g) => (
          <li key={g.cls}>
            <span className={`sq ${g.cls}`} aria-hidden="true" /> {g.versions.length === 1 ? <span className="mono">{g.label}</span> : g.label} {g.count}
          </li>
        ))}
      </ul>
    </div>
  );
}

function Restarts({ servers, spec, start, end }: { servers: ServerRow[]; spec: RangeSpec; start: number; end: number }) {
  const all = servers.flatMap((s) => s.restarts.map((t) => ({ t, name: s.name })));
  const who = [...new Set(all.map((r) => r.name))].sort((a, b) => byName({ name: a }, { name: b }));
  const span = end - start || 1;
  return (
    <div className="card dp">
      <span className="lbl">{titled("Restarts", spec)} <InfoTip label="Restarts" text="Observed server process restarts during the selected range. Each tick is a recorded start-time change; missing observations can hide additional restarts."/></span>
      <div className="dp-v">
        <b>{all.length}</b>
      </div>
      <svg className="dp-strip" viewBox="0 0 240 24" preserveAspectRatio="none" role="img" aria-label={all.length ? `Restarts at ${all.map((r) => `${hm(r.t)} UTC on ${r.name}`).join(", ")}` : "No restarts"}>
        <line x1="0" y1="23" x2="240" y2="23" className="dp-axis" vectorEffect="non-scaling-stroke" />
        {all.map((r) => (
          <rect key={r.name + r.t} x={Math.min(237, Math.max(0, ((r.t - start) / span) * 240)).toFixed(1)} y="6" width="3" height="17" className="dp-tick" />
        ))}
      </svg>
      <div className="dp-n">{who.length ? names(who) : "none"}</div>
    </div>
  );
}

function Store({ servers }: { servers: ServerRow[] }) {
  const withPct = servers.map((s) => ({ s, p: storePct(s) })).filter((x): x is { s: ServerRow; p: number } => x.p !== undefined);
  const med = median(withPct.map((x) => x.p));
  const lo = withPct.length ? Math.min(...withPct.map((x) => x.p)) : undefined;
  const top = withPct.length ? withPct.reduce((a, b) => (b.p > a.p ? b : a)) : undefined;
  const r = (v: number) => Math.round(v);
  // At least 1% wide, and never past the track's end.
  const bandLeft = lo === undefined ? 0 : Math.min(99, r(lo));
  return (
    <div className="card dp">
      <span className="lbl">JetStream store <InfoTip label="JetStream store" text="Configured JetStream file-storage allowance used on each server, as a percentage. The headline is the median; the band spans the lowest to highest reported percentage. Missing usage or nonpositive limits are excluded. This is not filesystem capacity."/></span>
      <div className="dp-v">
        <b>{med === undefined ? "–" : `${r(med)}%`}</b> {med !== undefined && <em>median</em>}
      </div>
      {med !== undefined && lo !== undefined && top && (
        <>
          <div className="dp-store" role="img" aria-label={`Store used: median ${r(med)}%, lowest ${r(lo)}%, highest ${r(top.p)}%`}>
            <span className="band" style={{ left: `${bandLeft}%`, width: `${Math.max(1, Math.min(100, r(top.p)) - bandLeft)}%` }} />
            <span className="med" style={{ left: `${Math.min(100, r(med))}%` }} />
          </div>
          <div className="dp-n">
            range {r(lo)} to {r(top.p)}% · fullest <span className={top.p >= STORE_ATTENTION_PCT ? "warn mono" : "mono"}>{top.s.name}</span>
          </div>
        </>
      )}
    </div>
  );
}

function SlowConsumers({ servers, spec, start, end }: { servers: ServerRow[]; spec: RangeSpec; start: number; end: number }) {
  const global = useGlobalQuery();
  const investigate = (from = start, to = end) => slowEvidencePath("/servers/slow-consumers", from, to, global);
  const b = useServersTrends(spec);
  useReportStale(b);
  const q = pickTrend(b, SERVERS_QUERIES, SLOW_STRIP_QUERY);
  const known = servers.filter((s) => s.slowConsumersInRange !== undefined);
  const total = known.reduce((a, s) => a + (s.slowConsumersInRange ?? 0), 0);
  const most = known.reduce<ServerRow | undefined>((a, s) => ((s.slowConsumersInRange ?? 0) > (a?.slowConsumersInRange ?? 0) ? s : a), undefined);
  const pts = q.data?.points ?? [];
  const peak = Math.max(1, ...pts.map((p) => p.value));
  const w = pts.length ? 240 / pts.length : 0;
  return (
    <div className="card dp">
      <span className="lbl">{titled("Slow-consumer detections", spec)} <InfoTip label="Slow-consumer detections" text="A detection means NATS had trouble sending data because a receiving connection could not keep up. Counts are known counter increases in the selected range, per server process. They cover clients, routes, gateways and leaf connections; retries can count without a disconnect. They are not unique clients or JetStream pending messages, and do not prove message loss."/></span>
      <div className="dp-v">
        <b className={total > 0 ? "warn" : undefined}>{known.length ? fmtInt(total) : "–"}</b>
      </div>
      <svg className="dp-strip" viewBox="0 0 240 24" preserveAspectRatio="none" role="group" aria-label={`Slow-consumer detections per ${pts.length ? "bucket" : "range"}, peak ${pts.length ? fmtInt(Math.max(...pts.map((p) => p.value))) : "none"}`}>
        <line x1="0" y1="23" x2="240" y2="23" className="dp-axis" vectorEffect="non-scaling-stroke" />
        {pts.map((p, i) => {
          const h = p.value > 0 ? 2 + (p.value / peak) * 18 : 0;
          const from = Math.max(start, p.time);
          const to = Math.min(end, p.time + (q.data?.stepMs ?? (pts.length > 1 ? pts[1].time - pts[0].time : end - start)));
          return h && to > from ? <Link key={p.time} to={investigate(from, to)} aria-label={`Investigate ${fmtInt(p.value)} ${p.value === 1 ? "detection" : "detections"} in bucket starting ${new Date(from).toISOString()}`}><rect x={(i * w).toFixed(1)} y={(23 - h).toFixed(1)} width={Math.max(1, w - 2).toFixed(1)} height={h.toFixed(1)} className="dp-bar" /></Link> : null;
        })}
      </svg>
      <Link className="x" to={investigate()}>Investigate detections</Link>
      <div className="dp-n">
        {!known.length ? "not reported" : total === 0 || !most ? "none" : (
          <>
            {fmtInt(most.slowConsumersInRange ?? 0)} of them on <Link className="x mono" to={slowEvidencePath(`/servers/${encodeURIComponent(most.name)}`, start, end, global)}>{most.name}</Link>
          </>
        )}
      </div>
    </div>
  );
}

export function ServerHeadline({ list, spec, start, end }: { list: ServerList; spec: RangeSpec; start: number; end: number }) {
  return (
    <div className="dp-row">
      <Answering list={list} />
      <Versions servers={list.servers} />
      <Restarts servers={list.servers} spec={spec} start={start} end={end} />
      <Store servers={list.servers} />
      <SlowConsumers servers={list.servers} spec={spec} start={start} end={end} />
    </div>
  );
}

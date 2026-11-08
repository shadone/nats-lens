import { useQuery } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router";
import { lens } from "../api/client";
import { ms } from "../api/adapt-common";
import { rangeInit } from "../api/requests";
import { QueryView, Section } from "../components/common";
import { useSystem } from "../lib/useSystem";
import { useTimeRange } from "../lib/useTimeRange";
import { LIVE_REFRESH_MS, rangeKey } from "../lib/timeRange";
import { useGlobalQuery } from "../lib/useGlobalQuery";
import { slowEvidencePath } from "../lib/slowConsumers";
import { usePageTitle } from "../lib/usePageTitle";

const when = (t: number | undefined) => t === undefined ? "unknown" : new Date(t).toISOString().replace(".000Z", "Z");

export function SlowConsumerInvestigation() {
  usePageTitle("Slow-consumer investigation");
  const { spec } = useTimeRange();
  const system = useSystem();
  const global = useGlobalQuery();
  const [params] = useSearchParams();
  const server = params.get("server") ?? "";
  const account = params.get("account") ?? "";
  // No placeholder across ranges: old evidence must never appear under new bounds.
  const q = useQuery({ queryKey: ["slow-consumer-investigation", system, rangeKey(spec), server, account], queryFn: ({ signal }) => lens.getSlowConsumerInvestigation({ system, range: rangeInit(spec), server, account, limit: 100 }, { signal }), refetchInterval: spec.kind === "live" ? LIVE_REFRESH_MS : false });
  return <>
    <div className="page-h"><h1>Slow-consumer investigation</h1></div>
    <p>A detection means NATS had trouble sending data because a receiving connection could not keep up.</p>
    <QueryView q={q} what="slow-consumer evidence">{d => {
      const start = ms(d.range?.start) ?? 0, end = ms(d.range?.end) ?? 0;
      const path = (to: string, from = start, until = end, name?: string) => slowEvidencePath(to, from, until, global, name);
      return <>
        <p>{when(start)} to {when(end)} · {server || "All servers"}{account ? ` · Recorded client evidence for ${account}; counters cover all accounts` : ""}</p>
        <p><b>{String(d.detections)} counter {d.detections === 1n ? "detection" : "detections"}</b> · <b>{String(d.recordedDisconnects)} recorded slow-consumer {d.recordedDisconnects === 1n ? "disconnect" : "disconnects"}</b></p>
        <p>{d.attributionLimit}</p>
        <Section title="Evidence coverage">
          <p role="note">{!d.eventsCaptured ? "Event capture history is unavailable; no recorded matches cannot rule out a disconnect." : d.captureGaps ? `Event history is incomplete (earliest stored event: ${when(ms(d.eventsStart))}). Missing matches cannot rule out a disconnect.` : "No known event-capture gaps in this range. No matching evidence means no matching recorded event, not proof that none occurred."}</p>
          <p>{String(d.incompleteCollections)} collections have partial or missing reports. Baselines, resets and gaps longer than three collection intervals remain unknown.</p>
          <Link className="x" to={path("/data-history")}>Inspect data history</Link>
        </Section>
        <Section title="Observed counter increases">
          <p>The interval lies between two reports from the same server process. It may begin before the selected range; the detection time within it is unknown.</p>
          {d.observations.length === 0 && <p>No counter increases observed in the available reports.</p>}
          <ul className="slow-evidence">{d.observations.map(o => <li key={`${o.serverId}/${o.epoch}`}>
            <Link className="x" to={path(`/servers/${encodeURIComponent(o.server)}`)}>{o.server}</Link>{" · "}
            {o.increase === undefined ? o.unavailableReason : `${String(o.increase)} ${o.increase === 1n ? "detection" : "detections"}`}
            <div>{when(ms(o.intervalStart))} to {when(ms(o.intervalEnd))}</div>
            <div className="mono">Process {o.serverId} · collection {String(o.epoch)}</div>
            {o.intervalStart && <Link className="x" to={path("/servers/slow-consumers", ms(o.intervalStart), (ms(o.intervalEnd) ?? end) + 1000, o.server)}>Investigate this interval</Link>}
          </li>)}</ul>
          {d.observationsTruncated && <p role="note">Observation list limited to 100 rows. Narrow the time range or server; totals include all matching observations.</p>}
        </Section>
        <Section title="Recorded disconnect evidence">
          <p>These events match the selected time, server and account. They do not explain each counter detection. Repeated events can name the same connection.</p>
          {d.events.length === 0 && <p>No matching recorded disconnects. nats-lens cannot identify the affected connection from the counter measurement. Open the affected server and check its diagnostics for the observed interval.</p>}
          <ul className="slow-evidence">{d.events.map(row => { const e = row.event!; return <li key={e.id}>
            <Link className="x" to={path(`/events?id=${encodeURIComponent(e.id)}`)}>Open recorded event {e.id}</Link>{" · "}{when(ms(e.time))}
            <div>{e.server} · {e.clientName || "Client name not recorded"} · {e.reason}</div>
            <div>{row.serverId && `Process ${row.serverId} · `}{row.connectionId ? `Connection ${row.connectionId} (recorded ID)` : "Connection ID not recorded"}</div>
          </li>; })}</ul>
          {d.eventsTruncated && <p role="note">Event list limited to 100 rows. Narrow the range or server; the disconnect total includes all matches.</p>}
        </Section>
        <Section title="Recorded findings">
          {d.findings.length === 0 && <p>No matching recorded slow-consumer finding transitions in this range.</p>}
          <ul className="slow-evidence">{d.findings.map(f => <li key={`${f.findingId}/${when(ms(f.time))}`}><Link className="x" to={path(`/findings/${encodeURIComponent(f.findingId)}`)}>{f.checkCode} · {f.entity?.name || f.entity?.key}</Link> · {when(ms(f.time))}<div>{f.detail}</div></li>)}</ul>
          {d.findingsTruncated && <p role="note">Finding list limited to 100 rows. Narrow the range.</p>}
        </Section>
        <details><summary>Evidence sources and metric help</summary><p>{d.provenance}</p><p>The aggregate covers clients, routes, gateways and leaf connections. Retries may count repeatedly without a disconnect. It is not unique JetStream consumers or pending messages.</p></details>
      </>;
    }}</QueryView>
  </>;
}

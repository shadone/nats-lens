// Cards of a server's page: where it runs, routes, limits and settings,
// recent events, and the monitoring endpoints that answered.
import type { ReactNode } from "react";
import type { RecentEvent, ServerInfo } from "../api/servers";
import { useServerEvents } from "../api/servers";
import { dayMonHm, eventWhen, fmtBytes, fmtDuration, fmtInt, fmtShortDuration } from "../lib/format";
import type { RangeSpec } from "../lib/timeRange";
import { eventPath, serverPath } from "../lib/view";
import { Dash, RLink } from "./common";

function Row({ label, children, differs }: { label: string; children: ReactNode; differs?: boolean }) {
  return (
    <>
      <dt>{label}</dt>
      <dd className={differs ? "differs" : undefined}>
        {children}
        {differs && <small>differs from most servers</small>}
      </dd>
    </>
  );
}

/** Pod and node from the Kubernetes watcher; nothing without it. */
export function WhereItRuns({ info }: { info: ServerInfo }) {
  const k = info.kubernetes;
  if (!k) return null;
  const zone = info.server?.tags.find((t) => t.startsWith("az:"))?.slice(3);
  return (
    <section className="card sd-card" aria-labelledby="sd-where">
      <h2 id="sd-where" className="srv-h2">
        Where it runs
      </h2>
      <dl className="sd-dl">
        <Row label="Pod">
          {k.pod}
          {k.namespace && ` in ${k.namespace}`} · {k.restartsInRange} {k.restartsInRange === 1 ? "restart" : "restarts"} in range
        </Row>
        <Row label="Node">{k.node || "not scheduled"}</Row>
        {k.scheduledAt !== undefined && <Row label="Scheduled">{dayMonHm(k.scheduledAt)} UTC</Row>}
        {zone && <Row label="Zone">{zone}</Row>}
      </dl>
    </section>
  );
}

export function Routes({ info }: { info: ServerInfo }) {
  const timed = info.routeViews.filter((r) => r.rttMs !== undefined);
  const slowest = timed.length ? timed.reduce((a, b) => (b.rttMs! > a.rttMs! ? b : a)) : undefined;
  const pending = info.routeViews.reduce((a, r) => a + (r.pendingBytes ?? 0), 0);
  return (
    <section className="card sd-card" aria-labelledby="sd-routes">
      <h2 id="sd-routes" className="srv-h2">
        Routes
      </h2>
      <dl className="sd-dl">
        <Row label="Routes">
          {info.routeViews.length ? fmtInt(info.routeViews.length) : info.routes !== undefined ? fmtInt(info.routes) : <Dash />}
          {slowest && (
            <>
              , slowest to <RLink to={serverPath(slowest.remoteName)}>{slowest.remoteName}</RLink> {fmtShortDuration(slowest.rttMs!)}
            </>
          )}
        </Row>
        {pending > 0 && <Row label="Pending">{fmtBytes(pending)} waiting on routes</Row>}
        <Row label="Gateways">{info.gateways !== undefined ? fmtInt(info.gateways) : <Dash />}</Row>
        <Row label="Leaf nodes">{info.leafnodes !== undefined ? fmtInt(info.leafnodes) : <Dash />}</Row>
        <Row label="Meta leader">{info.metaLeader ? <RLink to={serverPath(info.metaLeader)}>{info.metaLeader}</RLink> : <Dash />}</Row>
      </dl>
    </section>
  );
}

const opt = (v: number | undefined, f: (v: number) => string) => (v === undefined ? <Dash /> : f(v));

export function LimitsCard({ info }: { info: ServerInfo }) {
  const l = info.limits;
  const d = new Set(info.limitsDiffer);
  const n = info.limitsDiffer.length;
  const tags = info.server?.tags ?? [];
  return (
    <section className="card sd-card" aria-labelledby="sd-limits">
      <h2 id="sd-limits" className="srv-h2">
        Limits and settings
      </h2>
      {l ? (
        <dl className="sd-dl">
          <Row label="Max payload" differs={d.has("max_payload")}>
            {opt(l.maxPayload, fmtBytes)}
          </Row>
          <Row label="Max pending" differs={d.has("max_pending")}>
            {opt(l.maxPending, fmtBytes)}
          </Row>
          <Row label="Write deadline" differs={d.has("write_deadline")}>
            {opt(l.writeDeadlineMs, fmtDuration)}
          </Row>
          <Row label="Ping" differs={d.has("ping_interval") || d.has("max_pings_out")}>
            every {opt(l.pingIntervalMs, fmtDuration)} · {opt(l.maxPingsOut, (v) => fmtInt(v))} missed
          </Row>
          <Row label="JetStream" differs={d.has("js_max_storage") || d.has("js_max_memory")}>
            file {opt(l.jsMaxStorage, fmtBytes)} · memory {opt(l.jsMaxMemory, fmtBytes)}
          </Row>
          <Row label="Connections" differs={d.has("max_connections")}>
            {opt(l.maxConnections, (v) => fmtInt(v))}
          </Row>
          {tags.length > 0 && <Row label="Tags">{tags.join(", ")}</Row>}
        </dl>
      ) : (
        <p>Not reported.</p>
      )}
      {l && <p>{n === 0 ? "None differ from most servers." : `${n} ${n === 1 ? "setting differs" : "settings differ"} from most servers.`}</p>}
      {info.configLoadedAt !== undefined && <p>Configuration loaded {dayMonHm(info.configLoadedAt)} UTC.</p>}
    </section>
  );
}

function RecentRow({ e, end }: { e: RecentEvent; end: number }) {
  return (
    <li>
      <span className="t">{e.time !== undefined ? eventWhen(e.time, end) : "–"}</span>
      <span className={e.source === "kubernetes" ? "k8s" : undefined}>
        <RLink to={eventPath(e.id)}>{e.summary || e.kind}</RLink>
        {e.repeats > 1 && ` ×${e.repeats}`}
      </span>
    </li>
  );
}

export function RecentEvents({ name, spec, end }: { name: string; spec: RangeSpec; end: number }) {
  const q = useServerEvents(spec, name);
  return (
    <section className="card sd-card" aria-labelledby="sd-recent">
      <h2 id="sd-recent" className="srv-h2">
        Recent events
      </h2>
      {q.data === undefined ? (
        <p>{q.error ? "Could not load the events." : "Loading…"}</p>
      ) : q.data.length ? (
        <ol className="sd-recent">
          {q.data.map((e) => (
            <RecentRow key={e.id} e={e} end={end} />
          ))}
        </ol>
      ) : (
        <p>None in the range, routine ones left out.</p>
      )}
      <p>
        <RLink to={`/events?server=${encodeURIComponent(name)}`}>All events for this server</RLink>
      </p>
    </section>
  );
}

export function EndpointsAnswered({ info }: { info: ServerInfo }) {
  const ok = info.endpoints.filter((e) => e.state === "ok").length;
  const failed = info.endpoints.filter((e) => e.state !== "ok");
  if (!info.endpoints.length) return <p className="sd-ep muted-note">No monitoring request recorded in the newest epoch.</p>;
  return (
    <p className="sd-ep">
      <span className="mono">
        {ok}/{info.endpoints.length}
      </span>{" "}
      monitoring endpoints answered in the newest epoch
      {failed.length > 0 && (
        <>
          ; not:{" "}
          {failed.map((e, i) => (
            <span key={e.endpoint} className="bad" title={e.error || e.state}>
              {i > 0 && ", "}
              {e.endpoint} {e.state}
            </span>
          ))}
        </>
      )}
      .
    </p>
  );
}

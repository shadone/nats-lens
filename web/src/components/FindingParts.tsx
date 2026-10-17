// Finding pieces of the reference: worst finding card, all-clear card,
// tally and the rows of other findings.
import { Link } from "react-router";
import type { Entity, Finding, Tally as TallyModel } from "../api/model";
import { fmtDuration, hms, isoMinute, shortWhen } from "../lib/format";
import { entityName, entityPath, findingPath, findingTag, streamPath } from "../lib/view";
import { RLink } from "./common";
import { Glyph, Sev, StateTag } from "./Severity";

/** "consumer on stream stream", with links. */
export function EntityLine({ e, small }: { e?: Entity; small?: boolean }) {
  if (!e) return <span>system</span>;
  const path = entityPath(e);
  const name = entityName(e);
  const main = path ? <RLink to={path}>{name}</RLink> : <span>{name}</span>;
  if (e.kind !== "consumer") return main;
  const on = small ? <small>on</small> : <span>on stream</span>;
  return (
    <>
      {main} {on} <RLink to={streamPath(e.account, e.stream)}>{e.stream}</RLink>
    </>
  );
}

interface WorstProps {
  f: Finding;
  rangeStart?: number;
  end: number;
  onMute: (f: Finding) => void;
  queryString: string;
}

export function WorstFinding({ f, rangeStart, end, onMute, queryString }: WorstProps) {
  const since = f.since ?? end;
  return (
    <div className={`card worst ${f.severity}`}>
      <div className="k">
        <Sev level={f.severity} />
        <span className="code">{f.checkCode}</span>
        <span className="lbl">Worst current finding</span>
      </div>
      <h1>{f.checkName}</h1>
      <div className="ent">
        <EntityLine e={f.entity} />
      </div>
      <p>{f.detail}</p>
      <div className="meta">
        <div>
          <span className="lbl">First seen by nats-lens</span>
          <span className="v">
            {shortWhen(since, end)} UTC · {fmtDuration(end - since)}
          </span>
        </div>
        <div>
          <span className="lbl">Transition</span>
          <span className="v">
            <StateTag tag={findingTag(f, rangeStart)} flush />
          </span>
        </div>
        {f.cause && (
          <div>
            <span className="lbl">Cause in window</span>
            <span className="v">
              {f.cause.kind.toUpperCase()} {f.cause.time !== undefined ? hms(f.cause.time) : ""}
            </span>
          </div>
        )}
        <div className="btns">
          <Link className="btn p" to={`${findingPath(f.id)}${queryString}`}>
            Open finding
          </Link>
          <button type="button" className="btn" onClick={() => onMute(f)}>
            Mute…
          </button>
        </div>
      </div>
    </div>
  );
}

export function AllClear({ resolved, checksRun }: { resolved: number; checksRun: boolean }) {
  return (
    <div className="card worst clear">
      <div className="k">
        <span className="sev ok">All clear</span>
        <span className="lbl">No unmuted finding fires now</span>
      </div>
      <h1>Nothing needs attention</h1>
      <p>
        {checksRun
          ? `Every check passes for every entity, or the findings that fire are muted. ${resolved} resolved in the last 24 hours.`
          : "No check has reported a finding in this range. Checks may not have run yet; the status timeline below shows what ran."}
      </p>
    </div>
  );
}

function TallyRow({ g, label, v }: { g: "critical" | "warning" | "info" | "muted" | "resolved"; label: string; v: number }) {
  return (
    <div className="row">
      <Glyph level={g} />
      <span>{label}</span>
      <span className={`n${v ? "" : " zero"}`}>{v}</span>
    </div>
  );
}

export function Tally({ t }: { t: TallyModel }) {
  return (
    <div className="card tally" aria-label="Findings by severity">
      <TallyRow g="critical" label="Critical" v={t.critical} />
      <TallyRow g="warning" label="Warning" v={t.warning} />
      <TallyRow g="info" label="Info" v={t.info} />
      <TallyRow g="muted" label="Muted" v={t.muted} />
      <TallyRow g="resolved" label="Resolved, 24h" v={t.resolved24h} />
    </div>
  );
}

/** One row of the findings list; a muted one shows who muted it and why. */
export function FindingRow({ f, end, rangeStart }: { f: Finding; end: number; rangeStart?: number }) {
  const tag = findingTag(f, rangeStart);
  const m = f.mute;
  const since = f.since ?? end;
  return (
    <div className={`frow${m ? " mute" : ""}`}>
      <span>
        <Sev level={m ? "muted" : f.severity} />
      </span>
      <span className="code">{f.checkCode}</span>
      <span>
        <RLink to={findingPath(f.id)} className="t fl">
          {f.checkName}
        </RLink>
        {(tag === "FLAPPING" || tag === "RESOLVED" || tag === "CHANGED") && <StateTag tag={tag} />}
        <br />
        <span className="e">
          <EntityLine e={f.entity} small />
        </span>
      </span>
      <span className="d">{f.detail}</span>
      {m && m.expiresAt !== undefined ? (
        <span className="s">
          until {shortWhen(m.expiresAt, end)} {isoMinute(m.expiresAt).slice(11)}
          <br />
          in {fmtDuration(m.expiresAt - end)}
        </span>
      ) : (
        <span className="s">
          {f.state === "resolved" && f.resolvedAt !== undefined ? "resolved " : "since "}
          {shortWhen(f.state === "resolved" && f.resolvedAt !== undefined ? f.resolvedAt : since, end)}
          <br />
          {fmtDuration(end - since)}
        </span>
      )}
      {m && (
        <span className="mute-why">
          <b>Muted by {m.mutedBy || "unknown"}:</b> {m.reason}
          {m.expiresAt !== undefined && ` Expires ${isoMinute(m.expiresAt)} UTC.`}
        </span>
      )}
    </div>
  );
}

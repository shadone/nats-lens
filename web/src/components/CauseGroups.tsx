// The Overview's cause cards: unmuted firing findings grouped by their cause
// event, else by check and parent entity, with the entities, open and
// mute; and a strip of the muted findings with their reasons.
import type { Entity, Finding } from "../api/model";
import type { CauseGroup } from "../lib/causeGroups";
import { hms } from "../lib/format";
import { entityName, entityPath, eventPath, findingPath, streamPath } from "../lib/view";
import { RLink } from "./common";
import { EntityLine } from "./FindingParts";
import { Sev } from "./Severity";

/** Entities listed in a card before "N more". */
const SHOWN = 8;

const PLURAL: Record<Entity["kind"], [string, string]> = {
  system: ["system", "systems"],
  server: ["server", "servers"],
  account: ["account", "accounts"],
  stream: ["stream", "streams"],
  consumer: ["consumer", "consumers"],
  connection: ["connection", "connections"],
  client: ["client", "clients"],
  unknown: ["entity", "entities"],
};

/** The noun for n entities of one kind, else "entities". */
function nounOf(fs: Finding[]): string {
  const kinds = new Set(fs.map((f) => f.entity?.kind ?? "system"));
  const n = fs.length;
  return kinds.size === 1 ? PLURAL[[...kinds][0]][n === 1 ? 0 : 1] : n === 1 ? "entity" : "entities";
}

/**
 * "4 consumers on stream order-jobs", "2 streams in account APP",
 * "2 consumers on 2 streams", "3 servers". A parent is named only when
 * every entity has it.
 */
function whereLine(g: CauseGroup) {
  const n = g.findings.length;
  const noun = nounOf(g.findings);
  const first = g.findings[0].entity;
  if (g.parent?.kind === "stream" && first)
    return (
      <>
        {n} {noun} on stream <RLink to={streamPath(first.account, first.stream)}>{g.parent.name}</RLink>
      </>
    );
  if (g.parent?.kind === "account")
    return (
      <>
        {n} {noun} in account <span className="mono">{g.parent.name}</span>
      </>
    );
  if (!g.parent && g.findings.every((f) => f.entity?.kind === "consumer"))
    return (
      <>
        {n} {noun} on {g.parentCount} streams
      </>
    );
  if (!g.parent && g.findings.every((f) => f.entity?.kind === "stream"))
    return (
      <>
        {n} {noun} in {g.parentCount} accounts
      </>
    );
  return (
    <>
      {n} {noun}
    </>
  );
}

function EntityLink({ e }: { e?: Entity }) {
  if (!e) return <span>system</span>;
  const path = entityPath(e);
  return path ? <RLink to={path}>{entityName(e)}</RLink> : <span>{entityName(e)}</span>;
}

function Actions({ f, onMute, label }: { f: Finding; onMute: (f: Finding) => void; label: string }) {
  return (
    <span className="cg-act">
      <RLink to={findingPath(f.id)} className="btn sm" aria-label={`Open ${label}`}>
        Open
      </RLink>
      <button type="button" className="btn sm" onClick={() => onMute(f)} aria-label={`Mute ${label}`}>
        Mute…
      </button>
    </span>
  );
}

function CauseLine({ cause }: { cause: NonNullable<CauseGroup["cause"]> }) {
  return (
    <p className="cg-cause">
      Cause: <code>{cause.kind}</code>
      {cause.time !== undefined && <span className="mono"> {hms(cause.time)} UTC</span>} “{cause.summary}”{" "}
      <RLink to={eventPath(cause.id)}>event</RLink>
    </p>
  );
}

function findingLabel(f: Finding): string {
  return `${f.checkCode} on ${entityName(f.entity) || "system"}`;
}

/** One finding: the code row with the name, the detail, the entity and cause beside, the buttons. */
function SingleCard({ g, onMute }: { g: CauseGroup; onMute: (f: Finding) => void }) {
  const f = g.findings[0];
  const label = findingLabel(f);
  return (
    <article className={`card cg one ${g.severity}`} aria-label={`${f.checkName}: 1 finding`}>
      <div className="cg-main">
        <div className="cg-k">
          <Sev level={g.severity} />
          <span className="code">{f.checkCode}</span>
          <h3>{f.checkName}</h3>
        </div>
        <p className="cg-d">{f.detail}</p>
      </div>
      <div className="cg-side">
        <p className="cg-ent">
          <EntityLine e={f.entity} />
        </p>
        {g.cause && <CauseLine cause={g.cause} />}
      </div>
      <span className="cg-btns">
        <RLink to={findingPath(f.id)} className="btn p" aria-label={`Open ${label}`}>
          Open finding
        </RLink>
        <button type="button" className="btn" onClick={() => onMute(f)} aria-label={`Mute ${label}`}>
          Mute…
        </button>
      </span>
    </article>
  );
}

function CauseCard({ g, onMute }: { g: CauseGroup; onMute: (f: Finding) => void }) {
  const n = g.findings.length;
  if (n === 1) return <SingleCard g={g} onMute={onMute} />;
  const names = [...new Set(g.findings.map((f) => f.checkName))];
  const shown = g.findings.slice(0, SHOWN);
  return (
    <article className={`card cg ${g.severity}`} aria-label={`${names.join(", ")}: ${n} findings`}>
      <div className="cg-k">
        <Sev level={g.severity} />
        <span className="code">
          {g.checkCodes.join(", ")} × {n}
        </span>
        <h3>{names.join(" · ")}</h3>
        {g.cause && g.checkCodes.length > 1 && <span className="lbl">one cause</span>}
      </div>
      <p className="cg-where">{whereLine(g)}</p>
      {g.cause && <CauseLine cause={g.cause} />}
      <ul className="cg-list">
        {shown.map((f) => (
          <li key={f.id}>
            <span className="cg-n">
              <EntityLink e={f.entity} />
              {g.checkCodes.length > 1 && <small className="code"> {f.checkCode}</small>}
            </span>
            <span className="cg-d">{f.detail}</span>
            <Actions f={f} onMute={onMute} label={findingLabel(f)} />
          </li>
        ))}
        {n > shown.length && (
          <li className="cg-more">
            <RLink to={`/findings?check=${encodeURIComponent(g.checkCodes[0])}`}>{n - shown.length} more</RLink>
          </li>
        )}
      </ul>
    </article>
  );
}

/** Muted findings by reason: “reason” by who, then each finding. */
function MutedStrip({ muted }: { muted: Finding[] }) {
  const byReason = new Map<string, Finding[]>();
  for (const f of muted) {
    const k = `${f.mute?.reason ?? ""}\u0000${f.mute?.mutedBy ?? ""}`;
    byReason.set(k, [...(byReason.get(k) ?? []), f]);
  }
  return (
    <section className="card cg-muted" aria-label="Muted findings">
      <Sev level="muted" />
      <span className="cg-mn">
        {muted.length} muted {muted.length === 1 ? "finding" : "findings"}
      </span>
      <ul>
        {[...byReason.values()].map((fs) => {
          const m = fs[0].mute!;
          return (
            <li key={fs[0].id}>
              “{m.reason}” <span className="by">{m.mutedBy || "unknown"}</span>:{" "}
              {fs.map((f, i) => (
                <span key={f.id}>
                  {i > 0 && ", "}
                  <RLink to={findingPath(f.id)}>
                    {f.checkCode} {entityName(f.entity) || "system"}
                  </RLink>
                </span>
              ))}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

export function CauseGroups({ groups, muted, onMute }: { groups: CauseGroup[]; muted: Finding[]; onMute: (f: Finding) => void }) {
  if (groups.length === 0 && muted.length === 0) return null;
  return (
    <section className="causes" aria-label="Findings now">
      {groups.map((g) => (
        <CauseCard key={g.key} g={g} onMute={onMute} />
      ))}
      {muted.length > 0 && <MutedStrip muted={muted} />}
    </section>
  );
}

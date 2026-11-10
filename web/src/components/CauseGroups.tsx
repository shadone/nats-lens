import { AcknowledgementMark } from "./Acknowledgement";
import { usePageHistory } from "./HistoryNote";
import { atHistoryStart } from "../lib/historyView";
// The Overview's cause cards: unmuted firing findings grouped by their cause
// event, else by check and parent entity, with the entities, open and
// mute; and a strip of the muted findings with their reasons.
import { createContext, useContext } from "react";
import type { Entity, Finding } from "../api/model";
import { acceptable, distinctEntities, shownFindings } from "../lib/causeEvidence";
import { acrossAccounts, type CauseGroup } from "../lib/causeGroups";
import { hms } from "../lib/format";
import { streamLabel, accountLabeller, entityName, entityPath, eventPath, findingPath, streamPath } from "../lib/view";
import { RLink } from "./common";
import { AccountLabelContext, EntityLine } from "./FindingParts";
import { CauseEvidence, PlacementSummary, type PlacementRead } from "./CauseEvidence";
import { AcceptedTag, Sev } from "./Severity";

/** What every card shares: the placement read, the system, the account scope and the accept action. */
export interface CardContext {
  read: PlacementRead;
  system: string;
  scoped: boolean;
  /** Unset: no card offers acceptance. all ticks the finding's related entities. */
  onAccept?: (f: Finding, all?: boolean) => void;
}

const CardCtx = createContext<CardContext>({ read: { loading: false, failed: false }, system: "", scoped: false });

/** "Across accounts" on a card of findings of no one account, while the cards follow an account. */
function ScopeLabel({ g }: { g: CauseGroup }) {
  const { scoped } = useContext(CardCtx);
  return scoped && acrossAccounts(g) ? <span className="lbl cg-scope">Across accounts</span> : null;
}

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
function whereLine(g: CauseGroup, label: (id: string, name?: string) => string) {
  // Entities, not findings: one stream with two findings of one cause is one stream.
  const entities = distinctEntities(g.findings);
  const n = entities.length;
  const noun = nounOf(entities);
  const first = g.findings[0].entity;
  if (g.parent?.kind === "stream" && first)
    return (
      <>
        {n} {noun} on stream <RLink to={streamPath(first.account, first.stream)}>{streamLabel(g.parent.name)}</RLink>
      </>
    );
  if (g.parent?.kind === "account" && first)
    return (
      <>
        {n} {noun} in account <span className="mono">{label(first.account, first.accountName)}</span>
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
  const label = useContext(AccountLabelContext);
  if (!e) return <span>system</span>;
  const path = entityPath(e);
  const name = entityName(e, label);
  return path ? <RLink to={path}>{name}</RLink> : <span>{name}</span>;
}

function Actions({ f, onMute, label }: { f: Finding; onMute: (f: Finding) => void; label: string }) {
  const { onAccept } = useContext(CardCtx);
  return (
    <span className="cg-act">
      <RLink to={findingPath(f.id)} className="btn sm" aria-label={`Open ${label}`}>
        Open
      </RLink>
      <button type="button" className="btn sm" onClick={() => onMute(f)} aria-label={`Mute ${label}`}>
        Mute…
      </button>
      {onAccept && acceptable(f) && (
        <button type="button" className="btn sm" onClick={() => onAccept(f)} aria-label={`Accept ${label} as by design`}>
          Accept…
        </button>
      )}
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

function findingLabel(f: Finding, label: (id: string, name?: string) => string): string {
  return `${f.checkCode} on ${entityName(f.entity, label) || "system"}`;
}

/** One finding: the code row with the name, the detail and cause, the entity beside, the buttons. */
function SingleCard({ g, onMute }: { g: CauseGroup; onMute: (f: Finding) => void }) {
  const f = g.findings[0];
  const label = findingLabel(f, useContext(AccountLabelContext));
  const known = atHistoryStart(f, usePageHistory()?.historyStart);
  const { read, system, onAccept } = useContext(CardCtx);
  return (
    <article className={`card cg one ${g.severity}`} aria-label={`${f.checkName}: 1 finding`}>
      <div className="cg-main">
        <div className="cg-k">
          <Sev level={g.severity} />
          <span className="code">{f.checkCode}</span>
          <h3>{f.checkName}</h3>
          <ScopeLabel g={g} />
        </div>
        <p className="cg-d">{f.detail}</p>
        <AcknowledgementMark acknowledgement={f.acknowledgement} />
        {known && <p className="cg-d">already true when nats-lens started</p>}
        {g.cause && <CauseLine cause={g.cause} />}
        <CauseEvidence shown={g.findings} total={1} noun={nounOf(g.findings)} read={read} system={system} />
      </div>
      <div className="cg-side">
        <p className="cg-ent">
          <EntityLine e={f.entity} />
        </p>
      </div>
      <span className="cg-btns">
        <RLink to={findingPath(f.id)} className="btn p" aria-label={`Open ${label}`}>
          Open finding
        </RLink>
        <button type="button" className="btn" onClick={() => onMute(f)} aria-label={`Mute ${label}`}>
          Mute…
        </button>
        {onAccept && acceptable(f) && (
          <button type="button" className="btn" onClick={() => onAccept(f)} aria-label={`Accept ${label} as by design`}>
            Accept as by design…
          </button>
        )}
      </span>
    </article>
  );
}

function CauseCard({ g, onMute }: { g: CauseGroup; onMute: (f: Finding) => void }) {
  const accounts = useContext(AccountLabelContext);
  const historyStart = usePageHistory()?.historyStart;
  const { read, system } = useContext(CardCtx);
  const n = g.findings.length;
  if (n === 1) return <SingleCard g={g} onMute={onMute} />;
  const names = [...new Set(g.findings.map((f) => f.checkName))];
  const shown = shownFindings(g);
  const noun = nounOf(distinctEntities(shown));
  return (
    <article className={`card cg ${g.severity}`} aria-label={`${names.join(", ")}: ${n} findings`}>
      <div className="cg-k">
        <Sev level={g.severity} />
        <span className="code">
          {g.checkCodes.join(", ")} × {n}
        </span>
        <h3>{names.join(" · ")}</h3>
        {g.cause && g.checkCodes.length > 1 && <span className="lbl">One cause</span>}
        <ScopeLabel g={g} />
      </div>
      <p className="cg-where">{whereLine(g, accounts)}</p>
      {g.cause && <CauseLine cause={g.cause} />}
      <PlacementSummary shown={shown} noun={noun} read={read} />
      <ul className="cg-list">
        {shown.map((f) => (
          <li key={f.id}>
            <span className="cg-n">
              <EntityLink e={f.entity} />
              {g.checkCodes.length > 1 && <small className="code"> {f.checkCode}</small>}
            </span>
            <span className="cg-d">{f.detail}{atHistoryStart(f, historyStart) && <small> · <span>already true when nats-lens started</span></small>}<AcknowledgementMark acknowledgement={f.acknowledgement} /></span>
            <Actions f={f} onMute={onMute} label={findingLabel(f, accounts)} />
          </li>
        ))}
        {n > shown.length && (
          <li className="cg-more">
            <RLink to={`/findings?check=${encodeURIComponent(g.checkCodes[0])}`}>{n - shown.length} more</RLink>
          </li>
        )}
      </ul>
      <AcceptAll g={g} />
      <CauseEvidence shown={shown} total={n} noun={noun} read={read} system={system} />
    </article>
  );
}

/**
 * "Accept all N as by design…" on a card of one acceptable check under one
 * stream or account: the dialog opens with the card's siblings ticked.
 */
function AcceptAll({ g }: { g: CauseGroup }) {
  const { onAccept } = useContext(CardCtx);
  const label = useContext(AccountLabelContext);
  const p = g.parent?.kind;
  if (!onAccept || g.checkCodes.length !== 1 || (p !== "stream" && p !== "account") || !g.findings.every(acceptable)) return null;
  const f = g.findings[0];
  const entities = distinctEntities(g.findings);
  const where = p === "stream" ? `of stream ${streamLabel(g.parent!.name)}` : `of account ${label(f.entity!.account, f.entity!.accountName)}`;
  return (
    <p className="cg-all">
      <button type="button" className="btn sm" onClick={() => onAccept(f, true)} aria-label={`Accept ${f.checkCode} on all ${entities.length} ${nounOf(entities)} ${where} as by design`}>
        Accept all {entities.length} as by design…
      </button>
    </p>
  );
}

/**
 * Muted findings by reason: “reason” by who, then each finding. Accepted
 * findings have a strip of their own, with a link to their review list.
 */
function MutedStrip({ muted, accepted = false }: { muted: Finding[]; accepted?: boolean }) {
  const accounts = useContext(AccountLabelContext);
  const byReason = new Map<string, Finding[]>();
  for (const f of muted) {
    const k = `${f.mute?.reason ?? ""}\u0000${f.mute?.mutedBy ?? ""}`;
    byReason.set(k, [...(byReason.get(k) ?? []), f]);
  }
  const what = accepted ? "accepted" : "muted";
  return (
    <section className="card cg-muted" aria-label={accepted ? "Accepted findings" : "Muted findings"}>
      {accepted ? <AcceptedTag /> : <Sev level="muted" />}
      <span className="cg-mn">
        {muted.length} {what} {muted.length === 1 ? "finding" : "findings"}
      </span>
      {accepted && <RLink to="/findings/accepted">Review accepted findings</RLink>}
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
                    {f.checkCode} {entityName(f.entity, accounts) || "system"}
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

export function CauseGroups({ groups, muted, onMute, card }: { groups: CauseGroup[]; muted: Finding[]; onMute: (f: Finding) => void; card?: CardContext }) {
  if (groups.length === 0 && muted.length === 0) return null;
  // Every account the cards name: account entities and the accounts of streams and consumers.
  const label = accountLabeller([...groups.flatMap((g) => g.findings), ...muted].flatMap((f) => (f.entity?.account ? [f.entity] : [])));
  const accepted = muted.filter((f) => f.mute?.accepted);
  const plain = muted.filter((f) => !f.mute?.accepted);
  const body = (
    <AccountLabelContext.Provider value={label}>
      <section className="causes" aria-labelledby="causes-h">
        <h2 id="causes-h" className="sr-only">
          Findings
        </h2>
        {groups.map((g) => (
          <CauseCard key={g.key} g={g} onMute={onMute} />
        ))}
        {plain.length > 0 && <MutedStrip muted={plain} />}
        {accepted.length > 0 && <MutedStrip muted={accepted} accepted />}
      </section>
    </AccountLabelContext.Provider>
  );
  return card ? <CardCtx.Provider value={card}>{body}</CardCtx.Provider> : body;
}

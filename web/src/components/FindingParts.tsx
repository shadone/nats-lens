import { AcknowledgementMark } from "./Acknowledgement";
import { usePageHistory } from "./HistoryNote";
import { atHistoryStart } from "../lib/historyView";
// Finding pieces: the entity line and a row of the findings list.
import { createContext, useContext } from "react";
import type { Entity, Finding } from "../api/model";
import { fmtDuration, isoDate, isoMinute, shortWhen } from "../lib/format";
import { muteBy, reviewOn, sentence } from "../lib/muteView";
import { accountLabel, entityName, entityPath, findingPath, findingTag, streamPath } from "../lib/view";
import { RLink } from "./common";
import { Sev, StateTag } from "./Severity";

/** How a list labels its accounts (accountLabeller of its entities), so
 * two accounts of one name read apart; one account alone reads by name. */
export const AccountLabelContext = createContext<(id: string, name?: string) => string>(accountLabel);

/** "consumer on stream stream", with links. */
export function EntityLine({ e, small, label }: { e?: Entity; small?: boolean; label?: (id: string, name?: string) => string }) {
  const listLabel = useContext(AccountLabelContext);
  if (!e) return <span>system</span>;
  const path = entityPath(e);
  const name = entityName(e, label ?? listLabel);
  const main = path ? <RLink to={path}>{name}</RLink> : <span>{name}</span>;
  if (e.kind !== "consumer") return main;
  const on = small ? <small>on</small> : <span>on stream</span>;
  return (
    <>
      {main} {on} <RLink to={streamPath(e.account, e.stream)}>{e.stream}</RLink>
    </>
  );
}

/** One row of the findings list; a muted or accepted one shows who muted or accepted it and why. */
export function FindingRow({ f, end, rangeStart, label }: { f: Finding; end: number; rangeStart?: number; label?: (id: string, name?: string) => string }) {
  const known = atHistoryStart(f, usePageHistory()?.historyStart);
  const tag = findingTag(f, rangeStart);
  const m = f.mute;
  const since = f.since ?? end;
  return (
    <div className={`frow${m ? " mute" : ""}`}>
      <span>
        <Sev level={m ? "muted" : f.severity} label={m?.accepted ? "Accepted" : undefined} />
      </span>
      <span className="code">{f.checkCode}</span>
      <span>
        <RLink to={findingPath(f.id)} className="t fl">
          {f.checkName}
        </RLink>
        {(tag === "FLAPPING" || tag === "RESOLVED" || tag === "CHANGED") && <StateTag tag={tag} />}
        <br />
        <span className="e">
          <EntityLine e={f.entity} small label={label} />
        </span>
      </span>
      <span className="d">{f.detail}</span>
      {m && m.expiresAt !== undefined ? (
        <span className="s">
          {m.accepted ? (
            <>
              review on
              <br />
              {isoDate(m.expiresAt)}
            </>
          ) : (
            `until ${shortWhen(m.expiresAt, end)} ${isoMinute(m.expiresAt).slice(11)}`
          )}
          <br />
          in {fmtDuration(m.expiresAt - end)}
        </span>
      ) : known ? <span className="s">already true when nats-lens started</span> : (
        <span className="s">
          {f.state === "resolved" && f.resolvedAt !== undefined ? "resolved " : "since "}
          {shortWhen(f.state === "resolved" && f.resolvedAt !== undefined ? f.resolvedAt : since, end)}
          <br />
          {fmtDuration(end - since)}
        </span>
      )}
      {f.acknowledgement && <span className="oc-row-mark"><AcknowledgementMark acknowledgement={f.acknowledgement} /></span>}
      {m && (
        <span className="mute-why">
          <b>{muteBy(m)}:</b> {m.accepted ? `${sentence(m.reason)} ${reviewOn(m)}` : m.reason}
          {!m.accepted && m.expiresAt !== undefined && ` Expires ${isoMinute(m.expiresAt)} UTC.`}
        </span>
      )}
    </div>
  );
}

// One event of a feed. A row expands to the findings it raised (with
// remediation and related config), its fields, and its raw payload.
import { useState } from "react";
import type { Finding, LensEvent } from "../api/model";
import { useEvent } from "../api/queries";
import { eventWhen, fmtDuration, isoSecond } from "../lib/format";
import { selectionIntersects } from "../lib/textSelection";
import { AccountName } from "./AccountName";
import { SOURCE_CLASS, SOURCE_PILL, consumerPath, eventPath, findingPath, findingTag, serverPath, streamPath } from "../lib/view";
import { ErrorBox, KV, RLink, Remediation } from "./common";
import { Sev, StateTag } from "./Severity";

/** The badge of a finding event: NEW, RESOLVED, FLAPPING or CHANGED. */
export function eventBadge(e: LensEvent): string | undefined {
  if (e.source !== "finding") return undefined;
  const st = e.transition?.state ?? e.kind.split(".").pop();
  return st === "firing" ? "NEW" : st === "resolved" ? "RESOLVED" : st === "flapping" ? "FLAPPING" : st === "changed" ? "CHANGED" : undefined;
}

function RaisedFinding({ f, label }: { f: Finding; label: string }) {
  return (
    <>
      <div className="fhead">
        <span className="lbl">{label}</span>
        <Sev level={f.severity} />
        <span className="code">{f.checkCode}</span>
        <RLink to={findingPath(f.id)} className="x">
          <b>{f.checkName}</b>
        </RLink>
        <StateTag tag={findingTag(f, f.since)} />
        {f.value && (
          <span className="mv">
            <b>{f.value.display}</b>
            {f.value.name}
          </span>
        )}
      </div>
      {f.remediation && (
        <div className="box">
          <span className="lbl">Remediation</span>
          <Remediation text={f.remediation} />
        </div>
      )}
      {f.relatedConfig.length > 0 && (
        <div className="box cfg">
          <span className="lbl">Related config{f.relatedConfigSource ? `, from ${f.relatedConfigSource}` : ""}</span>
          <KV items={f.relatedConfig} />
          {f.value?.basis && <small>Metric basis: {f.value.basis}.</small>}
        </div>
      )}
    </>
  );
}

function Fields({ e }: { e: LensEvent & { repeats?: number; firstTime?: number } }) {
  const rows: [string, React.ReactNode][] = [];
  if (e.kind) rows.push(["kind", e.kind]);
  if (e.server) rows.push(["server", <RLink to={serverPath(e.server)}>{e.server}</RLink>]);
  if (e.account) rows.push(["account", <AccountName id={e.account} name={e.accountName} />]);
  if (e.stream) rows.push(["stream", e.account ? <RLink to={streamPath(e.account, e.stream)}>{e.stream}</RLink> : e.stream]);
  if (e.consumer)
    rows.push(["consumer", e.account && e.stream ? <RLink to={consumerPath(e.account, e.stream, e.consumer)}>{e.consumer}</RLink> : e.consumer]);
  if (e.clientName) rows.push(["client", <RLink to={`/connections?name=${encodeURIComponent(e.clientName)}`}>{e.clientName}</RLink>]);
  if (e.clientHost) rows.push(["client host", e.clientHost]);
  if (e.reason) rows.push(["reason", e.reason]);
  if ((e.repeats ?? 1) > 1) rows.push(["repeats", String(e.repeats)]);
  if ((e.repeats ?? 1) > 1 && e.firstTime !== undefined) rows.push(["first", `${isoSecond(e.firstTime)} UTC`]);
  if (e.time !== undefined) rows.push(["time", `${isoSecond(e.time)} UTC`]);
  if (e.receivedAt !== undefined) rows.push(["received", `${isoSecond(e.receivedAt)} UTC`]);
  return (
    <div className="box">
      <span className="lbl">Event</span>
      <dl className="kv">
        {rows.map(([k, v]) => (
          <div key={k} className="kv-pair">
            <dt>{k}</dt>
            <dd>{v}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

function Raw({ id }: { id: string }) {
  const q = useEvent(id);
  if (q.error) return <ErrorBox error={q.error} what="the payload" retry={() => q.refetch()} />;
  if (!q.data) return <p className="muted-note">Loading payload…</p>;
  let text = q.data.raw;
  try {
    text = JSON.stringify(JSON.parse(q.data.raw), null, 2);
  } catch {
    // Not JSON: shown as stored.
  }
  return <pre className="raw" tabIndex={0} aria-label="Raw payload">{text || "(empty)"}</pre>;
}

/** "×5 in 6 min" for a folded row, "×5" when its events share one time;
 * nothing for a single event. */
export function repeatText(repeats: number | undefined, firstTime: number | undefined, time: number | undefined): string | undefined {
  if (!repeats || repeats < 2) return undefined;
  const span = firstTime !== undefined && time !== undefined ? time - firstTime : 0;
  if (span < 1000) return `×${repeats}`;
  const s = Math.round(span / 1000);
  const within = s < 60 ? `${s} s` : s < 3600 ? `${Math.round(s / 60)} min` : fmtDuration(span);
  return `×${repeats} in ${within}`;
}

export function EventRow({ e, end, open, onToggle, compact }: { e: LensEvent & { repeats?: number; firstTime?: number }; end: number; open: boolean; onToggle: () => void; compact?: boolean }) {
  const [raw, setRaw] = useState(false);
  const badge = eventBadge(e);
  const sev = e.severity;
  const raised = e.raised.length ? e.raised : e.finding ? [e.finding] : [];
  const detailId = `ev-${e.id}`;
  const rep = repeatText(e.repeats, e.firstTime, e.time);
  return (
    <div className={`ev${open ? " open" : ""}${open && sev ? ` sev-${sev}` : ""}`}>
      <span className="t">{e.time !== undefined ? eventWhen(e.time, end) : "–"}</span>
      <span className={`src ${SOURCE_CLASS[e.source]}`}>{SOURCE_PILL[e.source]}</span>
      <div className="body">
        <button type="button" className="ev-toggle" aria-expanded={open} aria-controls={detailId} onClick={(event) => {
          // Native keyboard activation has detail 0 and must ignore stale selections.
          if (event.detail > 0 && selectionIntersects(event.currentTarget)) return;
          onToggle();
        }}>
          {badge && <span className={`badge ${badge} ${sev ?? ""}`}>{badge}</span>}
          <span className={`ty${sev === "critical" && e.source === "nats" ? " critical" : ""}`}>{e.type || e.kind}</span>
          <span className="tx">{e.summary}</span>
          {rep && <span className="rep mono">{rep}</span>}
        </button>
      </div>
      {open && (
        <div className="ex" id={detailId}>
          {raised.map((f) => (
            <RaisedFinding key={f.id} f={f} label={e.source === "finding" ? "Finding" : "Raised"} />
          ))}
          {(!compact || raised.length === 0) && <Fields e={e} />}
          <div className="raw-row">
            <button type="button" className="btn" aria-expanded={raw} onClick={() => setRaw((r) => !r)}>
              {raw ? "Hide raw payload" : "Raw payload"}
            </button>
            <RLink to={eventPath(e.id)} className="x">
              Link to this event
            </RLink>
          </div>
          {raw && <Raw id={e.id} />}
        </div>
      )}
    </div>
  );
}

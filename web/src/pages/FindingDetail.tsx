// One finding: what fails, remediation, related configuration, the cause,
// its mute, the entity's timeline and the finding's transitions.
import { useState } from "react";
import { useParams } from "react-router";
import type { Finding } from "../api/model";
import { useFinding, useLensInfo, useTimeline } from "../api/queries";
import { Empty, KV, QueryView, RLink, Remediation, Section } from "../components/common";
import { EntityLine } from "../components/FindingParts";
import { MuteDialog, UnmuteDialog } from "../components/MuteDialog";
import { Sev, StateTag } from "../components/Severity";
import { StatusTimeline } from "../components/StatusTimeline";
import { fmtDuration, isoMinute, isoSecond } from "../lib/format";
import { usePageTitle } from "../lib/usePageTitle";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";
import { eventPath, findingTag } from "../lib/view";

/** A firing finding first seen within a minute after the oldest stored
 * epoch: the condition may be older than nats-lens's history. */
export function atHistoryStart(f: Finding, historyStart: number | undefined): boolean {
  if (historyStart === undefined || f.since === undefined || f.state === "resolved") return false;
  return f.since >= historyStart && f.since - historyStart < 60_000;
}

function Head({ f, start, end, onMute, onUnmute }: { f: Finding; start: number; end: number; onMute: () => void; onUnmute: () => void }) {
  const since = f.since ?? end;
  const historyStart = useLensInfo().data?.historyStart;
  return (
    <div className={`card worst finding-h ${f.mute ? "info" : f.severity}`}>
      <div className="k">
        <Sev level={f.mute ? "muted" : f.severity} />
        <span className="code">{f.checkCode}</span>
        <StateTag tag={findingTag(f, start)} />
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
            {isoMinute(since)} UTC · {fmtDuration((f.resolvedAt ?? end) - since)}
            {atHistoryStart(f, historyStart) && ", first seen when nats-lens history begins"}
          </span>
        </div>
        {f.conditionSince !== undefined && (
          <div>
            <span className="lbl">Condition since</span>
            <span className="v">{isoMinute(f.conditionSince)} UTC</span>
          </div>
        )}
        {f.resolvedAt !== undefined && (
          <div>
            <span className="lbl">Resolved</span>
            <span className="v">{isoMinute(f.resolvedAt)} UTC</span>
          </div>
        )}
        <div>
          <span className="lbl">Transitions, last hour</span>
          <span className="v">{f.transitionsLastHour}</span>
        </div>
        {f.value && (
          <div>
            <span className="lbl">{f.value.name}</span>
            <span className="v">{f.value.display}</span>
          </div>
        )}
        {f.cause && (
          <div>
            <span className="lbl">Cause in window</span>
            <span className="v">
              <RLink to={eventPath(f.cause.id)}>
                {f.cause.kind.toUpperCase()} {f.cause.time !== undefined ? isoSecond(f.cause.time).slice(11) : ""}
              </RLink>
            </span>
          </div>
        )}
        <div className="btns">
          {f.mute ? (
            <button type="button" className="btn" onClick={onUnmute}>
              Lift mute…
            </button>
          ) : (
            <button type="button" className="btn p" onClick={onMute}>
              Mute…
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

export function FindingDetail() {
  const id = useParams().id ?? "";
  usePageTitle(id);
  const { spec } = useTimeRange();
  const { start, end } = useResolvedRange(spec);
  const q = useFinding(spec, id);
  const f = q.data?.finding;
  const tl = useTimeline(spec, f?.entity);
  const [muting, setMuting] = useState(false);
  const [lifting, setLifting] = useState(false);
  return (
    <>
      <nav className="crumbs" aria-label="Breadcrumb">
        <RLink to="/findings">Findings</RLink>
        <span aria-hidden="true">/</span>
        <span className="mono">{id}</span>
      </nav>
      <QueryView q={q} what="the finding">
        {(d) =>
          !d.finding ? (
            <Empty>No finding with this ID.</Empty>
          ) : (
            <>
              <section className="sec">
                <Head f={d.finding} start={start} end={end} onMute={() => setMuting(true)} onUnmute={() => setLifting(true)} />
              </section>
              {d.finding.mute && (
                <section className="sec card box-card">
                  <span className="lbl">Muted</span>
                  <p className="dlg-sub">
                    <b>Muted by {d.finding.mute.mutedBy || "unknown"}:</b> {d.finding.mute.reason}
                    {d.finding.mute.expiresAt !== undefined && ` Expires ${isoMinute(d.finding.mute.expiresAt)} UTC.`}
                  </p>
                </section>
              )}
              <section className="sec grid2 ex">
                <div className="box">
                  <span className="lbl">Remediation</span>
                  {d.finding.remediation ? <Remediation text={d.finding.remediation} /> : <p className="muted-note">The check gives no remediation.</p>}
                </div>
                <div className="box cfg">
                  <span className="lbl">Related config{d.finding.relatedConfigSource ? `, from ${d.finding.relatedConfigSource}` : ""}</span>
                  {d.finding.relatedConfig.length ? <KV items={d.finding.relatedConfig} /> : <p className="muted-note">None.</p>}
                  {d.finding.value?.basis && <small>Metric basis: {d.finding.value.basis}.</small>}
                </div>
              </section>
              <Section title="Status timeline" aside="Every check for this entity over the range.">
                <div className="card tl">
                  <QueryView q={tl} what="the timeline">
                    {(t) => (t.rows.length ? <StatusTimeline tl={t} end={end} live={spec.kind === "live"} asTable={false} /> : <Empty>No check rows for this entity.</Empty>)}
                  </QueryView>
                </div>
              </Section>
              <Section title="Transitions" aside="Newest first, at most 100.">
                {d.transitions.length ? (
                  <ol className="card tr-list">
                    {d.transitions.map((t, i) => (
                      <li key={i}>
                        <span className="t">{t.time !== undefined ? `${isoSecond(t.time)} UTC` : "–"}</span>
                        <span>
                          <Sev level={t.state === "resolved" ? "ok" : t.severity} label={t.state} />
                        </span>
                        <span>{t.detail}</span>
                      </li>
                    ))}
                  </ol>
                ) : (
                  <Empty>No transitions recorded.</Empty>
                )}
              </Section>
              <MuteDialog finding={muting ? d.finding : undefined} onClose={() => setMuting(false)} />
              <UnmuteDialog mute={lifting ? d.finding.mute : undefined} onClose={() => setLifting(false)} />
            </>
          )
        }
      </QueryView>
    </>
  );
}

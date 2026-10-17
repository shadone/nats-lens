// One finding: its headline, when the condition began and when nats-lens
// first saw it, each server's copy of its stream or consumer, what to do,
// what happened around its start, the entity's timeline, the finding's
// transitions and its mute.
import { useState } from "react";
import { useParams } from "react-router";
import { AROUND_MS, useEventsAround } from "../api/events";
import { useOtherFindings } from "../api/finding";
import type { Entity, Finding } from "../api/model";
import { useFinding, useLensInfo, useTimeline } from "../api/queries";
import { Empty, KV, QueryView, RLink, Remediation, Section } from "../components/common";
import { EventRow } from "../components/EventRow";
import { FindingEvidence } from "../components/FindingEvidence";
import { EntityLine } from "../components/FindingParts";
import { MuteDialog, UnmuteDialog } from "../components/MuteDialog";
import { Sev, StateTag } from "../components/Severity";
import { StatusTimeline } from "../components/StatusTimeline";
import { fmtDuration, isoMinute, isoSecond } from "../lib/format";
import { writeRange, type RangeSpec } from "../lib/timeRange";
import { usePageTitle } from "../lib/usePageTitle";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";
import { entityName, entityPath, eventPath, findingTag, serverPath, streamPath } from "../lib/view";

/**
 * A firing finding whose condition was already true when nats-lens
 * started: its condition began before the oldest stored epoch, or it was
 * first seen within a minute after it.
 */
export function atHistoryStart(f: Finding, historyStart: number | undefined): boolean {
  if (historyStart === undefined || f.since === undefined || f.state === "resolved") return false;
  if (f.conditionSince !== undefined && f.conditionSince < historyStart) return true;
  return f.since >= historyStart && f.since - historyStart < 60_000;
}

/** "Condition since X · first seen by nats-lens Y", and how many others share it. */
function When({ f, spec, historyStart }: { f: Finding; spec: RangeSpec; historyStart?: number }) {
  const others = useOtherFindings(spec, f).data ?? 0;
  const seen = <span className="mono">{f.since !== undefined ? `${isoMinute(f.since)} UTC` : "–"}</span>;
  return (
    <p className="fd-when">
      {f.conditionSince !== undefined ? (
        <>
          Condition since <span className="mono">{isoMinute(f.conditionSince)} UTC</span> · first seen by nats-lens {seen}
        </>
      ) : (
        <>First seen by nats-lens {seen}</>
      )}
      {atHistoryStart(f, historyStart) && ", already true when nats-lens started"}
      {others > 0 && (
        <>
          {" · "}
          <RLink to={`/findings?check=${encodeURIComponent(f.checkCode)}`}>
            {others} other {others === 1 ? "entity has" : "entities have"} this finding
          </RLink>
        </>
      )}
    </p>
  );
}

function Head({ f, spec, start, end, onMute, onUnmute }: { f: Finding; spec: RangeSpec; start: number; end: number; onMute: () => void; onUnmute: () => void }) {
  const since = f.since ?? end;
  const historyStart = useLensInfo().data?.historyStart;
  const name = entityName(f.entity);
  return (
    <div className={`card worst finding-h ${f.mute ? "info" : f.severity}`}>
      <div className="k">
        <Sev level={f.mute ? "muted" : f.severity} />
        <span className="code">{f.checkCode}</span>
        <StateTag tag={findingTag(f, start)} />
      </div>
      <h1>
        {f.checkName}
        {name && (
          <>
            : <span className="mono">{name}</span>
          </>
        )}
      </h1>
      <div className="ent">
        <EntityLine e={f.entity} />
      </div>
      {/* The check's own words, quoted as the API sends them. */}
      {f.detail && <p className="fd-detail">{f.detail}</p>}
      <When f={f} spec={spec} historyStart={historyStart} />
      <div className="meta">
        <div>
          <span className="lbl">{f.resolvedAt !== undefined ? "Fired for (since first seen)" : "Firing for (since first seen)"}</span>
          <span className="v">{f.since !== undefined ? fmtDuration((f.resolvedAt ?? end) - since) : "–"}</span>
        </div>
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

/** What happened within 10 minutes of the condition's start, or of the
 * first sighting: the whole system by default, since a cause is often
 * elsewhere, or only the finding's entity. */
function Around({ f, end, historyStart }: { f: Finding; end: number; historyStart?: number }) {
  const t = f.conditionSince ?? f.since;
  const [entityOnly, setEntityOnly] = useState(false);
  const beforeHistory = t !== undefined && historyStart !== undefined && t + AROUND_MS < historyStart;
  const entity = entityOnly ? f.entity : undefined;
  const q = useEventsAround(beforeHistory ? undefined : t, entity);
  const [open, setOpen] = useState<string | undefined>();
  if (t === undefined) return null;
  const link = eventsWindowLink(t - AROUND_MS, t + AROUND_MS, entity);
  return (
    <Section
      title="Around the start"
      aside={beforeHistory ? undefined : `10 minutes either side of ${isoMinute(t)} UTC.`}
      right={
        !beforeHistory && f.entity ? (
          <div className="grp" role="group" aria-label="Around the start scope">
            <button type="button" className={`chip${entityOnly ? "" : " on"}`} aria-pressed={!entityOnly} onClick={() => setEntityOnly(false)}>
              Whole system
            </button>
            <button type="button" className={`chip${entityOnly ? " on" : ""}`} aria-pressed={entityOnly} onClick={() => setEntityOnly(true)}>
              This entity
            </button>
          </div>
        ) : undefined
      }
      className="fd-around"
    >
      {beforeHistory ? (
        <p className="muted-note">nats-lens has no history before {isoMinute(historyStart!)} UTC, so it cannot say what happened then.</p>
      ) : (
        <QueryView q={q} what="the events around the start">
          {(d) => (
            <>
              {historyStart !== undefined && t - AROUND_MS < historyStart && <p className="muted-note">nats-lens has no history before {isoMinute(historyStart)} UTC.</p>}
              {d.items.length === 0 ? (
                <Empty>No events worth reading in these 20 minutes.</Empty>
              ) : (
                <div className="card evlist">
                  {d.items.map((e) => (
                    <EventRow key={e.id} e={e} end={end} open={open === e.id} onToggle={() => setOpen((o) => (o === e.id ? undefined : e.id))} compact />
                  ))}
                  <p className="pager">
                    <span>
                      Showing {d.items.length} of {d.totalCount} · <RLink to={link}>Open in Events</RLink>
                    </span>
                  </p>
                </div>
              )}
            </>
          )}
        </QueryView>
      )}
    </Section>
  );
}

/** The Events page over a fixed window, filtered to the entity when given. */
function eventsWindowLink(start: number, end: number, e: Entity | undefined): string {
  const filtered = e ? eventsLink(e) : undefined;
  const p = writeRange(new URLSearchParams(filtered ? filtered.split("?")[1] : ""), { kind: "fixed", from: start, to: end });
  return `/events?${p.toString()}`;
}

/** The events page filtered to the entity, when it can be. */
function eventsLink(e: Entity): string | undefined {
  switch (e.kind) {
    case "stream":
      return `/events?stream=${encodeURIComponent(`${e.account}/${e.stream}`)}`;
    case "consumer":
      return `/events?consumer=${encodeURIComponent(`${e.account}/${e.stream}/${e.consumer}`)}`;
    case "server":
      return `/events?server=${encodeURIComponent(e.server || e.key)}`;
    case "client":
      return `/events?client=${encodeURIComponent(e.key)}`;
    default:
      return undefined;
  }
}

function Related({ e }: { e: Entity | undefined }) {
  if (!e) return null;
  const own = entityPath(e);
  const events = eventsLink(e);
  return (
    <section className="card box-card" aria-label="Related">
      <span className="lbl">Related</span>
      <ul>
        {own && (
          <li>
            <RLink to={own}>{entityName(e)}</RLink> {e.kind}
          </li>
        )}
        {e.kind === "consumer" && (
          <li>
            <RLink to={streamPath(e.account, e.stream)}>{e.stream}</RLink> stream
          </li>
        )}
        {e.server && e.kind !== "server" && (
          <li>
            <RLink to={serverPath(e.server)}>{e.server}</RLink> server
          </li>
        )}
        {events && (
          <li>
            <RLink to={events}>Events for this {e.kind}</RLink>
          </li>
        )}
      </ul>
    </section>
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
  const historyStart = useLensInfo().data?.historyStart;
  const [muting, setMuting] = useState(false);
  const [lifting, setLifting] = useState(false);
  return (
    <>
      <nav className="crumbs" aria-label="Breadcrumb">
        <RLink to="/findings">Findings</RLink>
        <span aria-hidden="true">/</span>
        {f ? (
          <>
            <RLink to={`/findings?check=${encodeURIComponent(f.checkCode)}`}>{f.checkName}</RLink>
            <span aria-hidden="true">/</span>
            <span className="mono">{entityName(f.entity) || "system"}</span>
          </>
        ) : (
          <span className="mono">{id}</span>
        )}
      </nav>
      <QueryView q={q} what="the finding">
        {(d) =>
          !d.finding ? (
            <Empty>No finding with this ID.</Empty>
          ) : (
            <>
              <section className="sec">
                <Head f={d.finding} spec={spec} start={start} end={end} onMute={() => setMuting(true)} onUnmute={() => setLifting(true)} />
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
              <div className="sec fd-cols">
                <div className="fd-main">
                  <FindingEvidence spec={spec} entity={d.finding.entity} />
                  <section className="grid2 ex" aria-label="What to do">
                    <div className="box">
                      <span className="lbl">Remediation</span>
                      {d.finding.remediation ? <Remediation text={d.finding.remediation} /> : <p className="muted-note">The check gives no remediation.</p>}
                    </div>
                    <div className="box cfg">
                      <span className="lbl">Related settings{d.finding.relatedConfigSource ? `, from ${d.finding.relatedConfigSource}` : ""}</span>
                      {d.finding.relatedConfig.length ? <KV items={d.finding.relatedConfig} /> : <p className="muted-note">None.</p>}
                      {d.finding.value?.basis && <small>Metric basis: {d.finding.value.basis}.</small>}
                    </div>
                  </section>
                  <Around f={d.finding} end={end} historyStart={historyStart} />
                </div>
                <aside className="fd-side">
                  <Related e={d.finding.entity} />
                </aside>
              </div>
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

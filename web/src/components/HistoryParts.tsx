// The Data history page's cards: the coverage of the range, the gaps as a
// strip and a table, what uses the history, and the archive.
import type { ReactNode } from "react";
import { isHatched, type ArchiveState, type DataHistory, type HistoryGap } from "../api/history";
import { fmtInt } from "../lib/format";
import {
  ARCHIVE_NOTE,
  archiveHoursText,
  archiveKeptText,
  coveredShare,
  coveredSpanText,
  enoughState,
  gapCountLines,
  gapDetail,
  gapEndText,
  gapLabel,
  gapLengthText,
  gapName,
  gapTime,
  historyUseLabel,
  historyUseLine,
  lastErrorText,
  lastExportText,
  retentionLine,
  shareText,
  spanText,
  stripEnds,
  stripSegments,
  whenText,
} from "../lib/historyView";
import { rangeEndingAt } from "../lib/replayView";
import { eventPath } from "../lib/view";
import { Dash, RLink } from "./common";
import { InfoTip } from "./MetricBox";

function Card({ title, sub, children, className }: { title: string; sub?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={`card dh-card ${className ?? ""}`} aria-label={title}>
      <div className="dh-card-h">
        <h2>{title}</h2>
        {sub && <p className="dh-sub">{sub}</p>}
      </div>
      {children}
    </section>
  );
}

/** A box in the metric box's look, without a chart: a title with (i), the value, and a note. */
function Box({ title, description, value, note }: { title: string; description: string; value?: string; note?: ReactNode }) {
  return (
    <div className="card mbox dh-box">
      <div className="mb-h">
        <span className="lbl">
          <span className="mb-t">{title}</span>
          <InfoTip label={title} text={description} />
        </span>
        <span className="mb-v">
          <b className="mono">{value ?? <Dash />}</b>
        </span>
      </div>
      {note && <div className="mb-note">{note}</div>}
    </div>
  );
}

/** Collections, partial collections, time without data and the share covered, over the covered span. */
export function CoverageBoxes({ d }: { d: DataHistory }) {
  const c = d.coverage;
  const span = coveredSpanText(c);
  if (span === undefined) return <p className="dh-empty">No collection in this range: nats-lens holds nothing of it.</p>;
  const share = coveredShare(c);
  const started = d.historyStart !== undefined && c.from !== undefined && c.from > d.rangeStart;
  return (
    <div className="dh-boxes">
      <Box
        title="Collections"
        description="Collections that started in the range, and how many the collect interval gives over the covered span."
        value={fmtInt(c.epochs)}
        note={`of ${fmtInt(c.expected)} expected, ${span}`}
      />
      <Box title="Partial collections" description="Collections in the range that missed a server." value={fmtInt(c.partial)} note={`missed a server, ${span}`} />
      <Box
        title="Without data"
        description="The time without collections within the covered span: nats-lens not running, not reaching NATS, or not collecting."
        value={c.withoutData === undefined ? undefined : spanText(c.withoutData)}
        note={span}
      />
      <Box
        title="Covered"
        description="The share of the covered span with collections. The covered span starts at the later of the range start and the history start."
        value={share === undefined || c.withoutData === undefined ? undefined : shareText(share, c.withoutData, c.epochs)}
        note={started ? `${span}, from the history start` : span}
      />
    </div>
  );
}

/** A collection either side of a gap, linked to Placement over the hour that ends just after it. */
function EpochLink({ side, epoch, at, rangeEnd }: { side: string; epoch?: string; at?: number; rangeEnd: number }) {
  if (epoch === undefined) return null;
  const text = at === undefined ? `collection ${epoch}` : `collection ${epoch}, ${gapTime(at, rangeEnd)}`;
  return (
    <span className="dh-epoch">
      {side}{" "}
      {at === undefined ? text : <RLink to={`/placement?${rangeEndingAt(at)}`}>{text}</RLink>}
    </span>
  );
}

function GapRow({ g, rangeEnd, live }: { g: HistoryGap; rangeEnd: number; live: boolean }) {
  const detail = gapDetail(g);
  const length = gapLengthText(g, rangeEnd, live);
  const either = g.beforeEpoch !== undefined || g.afterEpoch !== undefined;
  return (
    <tr className={isHatched(g) ? "dh-hatched-row" : undefined}>
      <th scope="row">
        <span className={`dh-swatch${isHatched(g) ? " dh-hatch" : ""}`} aria-hidden="true" />
        {gapLabel(g.kind)}
        {detail && <small>{detail}</small>}
      </th>
      <td className="mono">{g.start === undefined ? <Dash /> : gapTime(g.start, rangeEnd)}</td>
      <td className={`mono${g.end === undefined ? " dh-open" : ""}`}>{gapEndText(g, rangeEnd, live)}</td>
      <td className="mono">{length ?? <Dash />}</td>
      <td className="dh-either">
        {either ? (
          <>
            <EpochLink side="before:" epoch={g.beforeEpoch} at={g.beforeEpochAt} rangeEnd={rangeEnd} />
            <EpochLink side="after:" epoch={g.afterEpoch} at={g.afterEpochAt} rangeEnd={rangeEnd} />
          </>
        ) : (
          <Dash />
        )}
      </td>
      <td className="mono">{g.eventId ? <RLink to={eventPath(g.eventId)}>event {g.eventId}</RLink> : <Dash />}</td>
    </tr>
  );
}

/** A gap's key: two hatched gaps never share a kind and a start. */
function gapKey(g: HistoryGap): string {
  return `${g.kind}|${g.start ?? ""}|${g.eventId}`;
}

/** The range with its hatched gaps, oldest left. */
export function GapStrip({ d, live }: { d: DataHistory; live: boolean }) {
  const segs = stripSegments(d);
  const ends = stripEnds(d, live);
  const n = segs.length;
  const name = n === 0 ? "The range, with collections all through" : `The range, with ${n} ${n === 1 ? "time" : "times"} without collections hatched`;
  return (
    <div className="dh-strip-wrap">
      <div className="dh-strip" role="img" aria-label={name}>
        {segs.map((s) => (
          <span key={gapKey(s.gap)} className="dh-seg dh-hatch" style={{ left: `${s.left}%`, width: `${s.width}%` }} title={gapName(s.gap, d.rangeEnd, live)} />
        ))}
      </div>
      <div className="dh-strip-ends" aria-hidden="true">
        <span>{ends[0]}</span>
        <span>{ends[1]}</span>
      </div>
      {n > 0 && (
        <ul className="sr-only">
          {segs.map((s) => (
            <li key={gapKey(s.gap)}>{gapName(s.gap, d.rangeEnd, live)}</li>
          ))}
        </ul>
      )}
    </div>
  );
}

export function GapsCard({ d, live }: { d: DataHistory; live: boolean }) {
  const lines = gapCountLines(d);
  return (
    <Card title="Gaps" sub="Times nats-lens holds nothing, or only part, of the range, newest first. Hatched times have no collections; charts draw them as gaps." className="dh-gaps">
      <GapStrip d={d} live={live} />
      {d.gaps.length === 0 ? (
        <p className="dh-empty">No gaps in this range.</p>
      ) : (
        <div className="table-wrap scroll-cue">
          <table className="tbl dh-tbl">
            <thead>
              <tr>
                <th scope="col">Kind</th>
                <th scope="col">From (UTC)</th>
                <th scope="col">To (UTC)</th>
                <th scope="col">Length</th>
                <th scope="col">Collections either side</th>
                <th scope="col">Event</th>
              </tr>
            </thead>
            <tbody>
              {d.gaps.map((g, i) => (
                <GapRow key={`${g.kind}|${g.start}|${g.eventId}|${i}`} g={g} rangeEnd={d.rangeEnd} live={live} />
              ))}
            </tbody>
          </table>
        </div>
      )}
      {lines.length > 0 && (
        <p className="dh-foot">
          {lines.map((l) => (
            <span key={l}>{l} </span>
          ))}
        </p>
      )}
    </Card>
  );
}

export function UsesCard({ d, now }: { d: DataHistory; now: number }) {
  return (
    <Card title="What uses history" sub="Global collection sample spans, which may contain gaps. Growth uses the selected range; other windows end at the latest collection. Bounded targets may use observed samples on either side of the start, each within one collection interval; the displayed span stays exact. Entity measurements and algorithm rules still determine each result. Forecast targets are configured lookbacks; rates may use shorter spans when their minimum span and slope rules pass. Acceptance review needs its baseline buckets; daily totals can be incomplete, and Replay only visits stored moments." className="dh-uses">
      {d.uses.length === 0 ? (
        <p className="dh-empty">Nothing reported.</p>
      ) : (
        <div className="table-wrap scroll-cue">
          <table className="tbl dh-tbl">
            <thead>
              <tr>
                <th scope="col">Use</th>
                <th scope="col">Target span</th>
                <th scope="col">Sample span</th>
                <th scope="col">Availability</th>
              </tr>
            </thead>
            <tbody>
              {d.uses.map((u) => {
                const l = historyUseLabel(u);
                const st = enoughState(u, d.retention, now);
                return (
                  <tr key={u.id} title={historyUseLine(u, d.retention, now)}>
                    <th scope="row">
                      {l.title}
                      {l.sub && <small>{l.sub}</small>}
                    </th>
                    <td className="mono">{u.needs === undefined ? <Dash /> : spanText(u.needs)}</td>
                    <td className="mono">{u.has === undefined ? <Dash /> : spanText(u.has)}</td>
                    <td className={`dh-${st.tone}`}>{st.text}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}

/** Text with `code` in backticks shown as code. */
function Coded({ text }: { text: string }) {
  return <>{text.split(/(`[^`]+`)/g).map((p, i) => (p.length > 1 && p.startsWith("`") && p.endsWith("`") ? <code key={i}>{p.slice(1, -1)}</code> : p))}</>;
}

function ArchiveList({ a, now }: { a: ArchiveState; now: number }) {
  const err = lastErrorText(a, now);
  const rows: [string, ReactNode][] = [
    ["Location", <span className="mono dh-loc">{a.location || <Dash />}</span>],
    ["Hours", <span className="mono">{archiveHoursText(a)}</span>],
    ["Kept", <span className="mono">{archiveKeptText(a)}</span>],
    ["Last export", <span className="mono">{lastExportText(a, now)}</span>],
  ];
  if (a.listedAt !== undefined) rows.push(["Listed", <span className="mono">{whenText(a.listedAt, now)}</span>]);
  return (
    <>
      <dl className="dh-kv">
        {rows.map(([k, v]) => (
          <div key={k}>
            <dt>{k}</dt>
            <dd>{v}</dd>
          </div>
        ))}
      </dl>
      {err && (
        <p className="dh-error" role="note">
          {err}
        </p>
      )}
    </>
  );
}

export function ArchiveCard({ d, now }: { d: DataHistory; now: number }) {
  const a = d.archive;
  return (
    <Card title="Retention and archive" sub={retentionLine(d, now)} className="dh-archive">
      {a.enabled ? (
        <ArchiveList a={a} now={now} />
      ) : (
        <p className="dh-empty">
          <Coded text="No archive: `archive_dir` is not set for this system, so nothing is archived." />
        </p>
      )}
      <p className="dh-foot">
        <Coded text={ARCHIVE_NOTE} />
      </p>
    </Card>
  );
}

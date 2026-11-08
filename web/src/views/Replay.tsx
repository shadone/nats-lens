import { HistoryLink } from "../components/HistoryNote";
import type { ReplayModel } from "../pages/Replay";
// Replay: what changed between two moments A and B, and in what order. A
// and B live in the URL beside the range and the system; the page picks
// them on the range's status strip and tells the story GetReplay returns.
import type { Replay as ReplayData } from "../api/adapt-replay";
import { ErrorBox, Loading, errorText } from "../components/common";
import { ReplayCounts } from "../components/ReplayCounts";
import { ReplayGone } from "../components/ReplayGone";
import { ReplayScrubber } from "../components/ReplayScrubber";
import { ReplaySteps } from "../components/ReplaySteps";
import { dayMonHm } from "../lib/format";
import { type Moments } from "../lib/replayUrl";
import { collectionNotes, stepNotes, whenFormat } from "../lib/replayView";

/** "09:14–09:50 UTC 17 Oct" */
function spanText(m: Moments): string {
  return `${dayMonHm(m.a)}–${dayMonHm(m.b)} UTC`;
}

function goneRead(r: ReplayData): boolean {
  const c = r.counts;
  return c.streamsGoneWithoutDelete !== undefined || c.consumersGoneWithoutDelete !== undefined || c.assetsGoneWithDelete !== undefined || r.gone.length > 0;
}

export function ReplayView({ model }: { model: ReplayModel }) {
  const { spec, range, now, minGapMs, strip, moments, linkNotes, setMoments, q, data, current, settledNow, status, copy, exportLog } = model;

  let body;
  if (!moments) {
    body = strip.isPending ? <Loading what="the range's Kubernetes windows" /> : null;
  } else if (q.error && settledNow) {
    body = <ErrorBox error={q.error} retry={() => q.refetch()} what="the replay" />;
  } else if (data === undefined) {
    body = <Loading what="the replay" />;
  } else {
    const shown = { a: data.a, b: data.b };
    const notes = collectionNotes(data);
    body = (
      <>
        {!current && (
          <p className="rp-loading" role="status">
            <span className="spinner" aria-hidden="true" /> Loading {spanText(moments)}; below is the earlier choice, {spanText(shown)}.
          </p>
        )}
        <div className={`rp-story${current ? "" : " stale"}`} aria-busy={!current} inert={!current}>
          {notes.length > 0 && (
            <div className="rp-notes card" role="note">
              {notes.map((n) => (
                <p key={n}>{n.startsWith("A is before the history") ? <HistoryLink>{n}</HistoryLink> : n}</p>
              ))}
            </div>
          )}
          <div className="rp-grid">
            <ReplaySteps steps={data.steps} moments={shown} notes={stepNotes(data)} />
            <ReplayCounts counts={data.counts} unobserved={data.unobserved} fmt={whenFormat(shown)} />
          </div>
          <ReplayGone gone={data.gone} truncated={data.goneTruncated} moments={shown} read={goneRead(data)} />
        </div>
      </>
    );
  }

  return (
    <>
      <div className="page-h">
        <span className="aside">Pick two moments; nats-lens tells you what changed in between, and in what order.</span>
        <div className="right">
          <button type="button" className="btn" onClick={copy} disabled={!moments}>
            Copy link to A–B
          </button>
          <button type="button" className="btn" onClick={exportLog} disabled={!current}>
            Export as incident log
          </button>
        </div>
      </div>
      <p className="rp-status" role="status">
        {status}
      </p>
      {moments && (
        <ReplayScrubber
          start={range.start}
          end={range.end}
          live={spec.kind === "live"}
          now={now}
          strip={strip.data}
          stripError={strip.error ? errorText(strip.error) : undefined}
          moments={moments}
          minGapMs={minGapMs}
          onChange={setMoments}
        />
      )}
      {linkNotes.length > 0 && (
        <p className="rs-note" role="note">
          {linkNotes.join(" ")}
        </p>
      )}
      {body}
    </>
  );
}

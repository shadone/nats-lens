// Replay: what changed between two moments A and B, and in what order. A
// and B live in the URL beside the range and the system; the page picks
// them on the range's status strip and tells the story GetReplay returns.
import { serverNow } from "../lib/clock";
import { useEffect, useMemo, useState } from "react";
import { useLocation, useSearchParams } from "react-router";
import type { Replay as ReplayData } from "../api/adapt-replay";
import { useLensInfo } from "../api/queries";
import { useReplay, useReplayStrip, useSettledMoments } from "../api/replay";
import { ErrorBox, Loading, errorText } from "../components/common";
import { ReplayCounts } from "../components/ReplayCounts";
import { ReplayGone } from "../components/ReplayGone";
import { ReplayScrubber } from "../components/ReplayScrubber";
import { ReplaySteps } from "../components/ReplaySteps";
import { dayMonHm } from "../lib/format";
import { SYSTEM_PARAM, ACCOUNT_PARAM } from "../lib/globalQuery";
import { logFileName, replayLog } from "../lib/replayLog";
import { A_PARAM, B_PARAM, momentIso, resolveMoments, shareParams, writeMoments, type Moments } from "../lib/replayUrl";
import { collectionNotes, stepNotes, whenFormat } from "../lib/replayView";
import { pauseRange } from "../lib/timeRange";
import { useGlobalQuery } from "../lib/useGlobalQuery";
import { usePageTitle } from "../lib/usePageTitle";
import { useSystem } from "../lib/useSystem";
import { useNow, useResolvedRange, useTimeRange } from "../lib/useTimeRange";

/** "09:14–09:50 UTC 17 Oct" */
function spanText(m: Moments): string {
  return `${dayMonHm(m.a)}–${dayMonHm(m.b)} UTC`;
}

/** Saves text as a file through a temporary link. */
function download(name: string, text: string) {
  const href = URL.createObjectURL(new Blob([text], { type: "text/plain;charset=utf-8" }));
  const el = document.createElement("a");
  el.href = href;
  el.download = name;
  document.body.appendChild(el);
  el.click();
  el.remove();
  setTimeout(() => URL.revokeObjectURL(href), 0);
}

function goneRead(r: ReplayData): boolean {
  const c = r.counts;
  return c.streamsGoneWithoutDelete !== undefined || c.consumersGoneWithoutDelete !== undefined || c.assetsGoneWithDelete !== undefined || r.gone.length > 0;
}

export function Replay() {
  usePageTitle("Replay");
  const [params, setParams] = useSearchParams();
  const { pathname } = useLocation();
  const { spec, setSpec } = useTimeRange();
  const sys = useSystem();
  const global = useGlobalQuery();
  const range = useResolvedRange(spec);
  const now = useNow(30_000);
  const minGapMs = useLensInfo().data?.collectIntervalMs ?? 30_000;
  const strip = useReplayStrip(spec);
  // The defaults wait for the strip's windows; a failed strip has none.
  const windows = strip.data ? strip.data.windows : strip.error ? [] : undefined;
  const rawA = params.get(A_PARAM);
  const rawB = params.get(B_PARAM);
  const resolved = useMemo(
    () => resolveMoments(rawA, rawB, { windows, range, now, minGapMs }),
    [rawA, rawB, windows, range.start, range.end, now, minGapMs],
  );
  const moments: Moments | undefined = resolved && { a: resolved.a, b: resolved.b };

  // A link without A or B, or with moments GetReplay does not accept, gets
  // the moments read from it written into it, without a history entry of
  // its own, so they no longer move with the clock. What was changed stays
  // said for as long as the link is the one written.
  const [kept, setKept] = useState<{ link: string; notes: string[] }>({ link: "", notes: [] });
  useEffect(() => {
    if (!resolved?.write) return;
    setKept({ link: `${momentIso(resolved.a)}|${momentIso(resolved.b)}`, notes: resolved.notes });
    setParams((p) => writeMoments(p, resolved), { replace: true });
  }, [resolved?.write, resolved?.a, resolved?.b]);
  const linkNotes = kept.link === `${rawA}|${rawB}` ? kept.notes : (resolved?.notes ?? []);

  // A move of the user's own ends what the link's notes said.
  const setMoments = (m: Moments) => {
    setKept({ link: "", notes: [] });
    setParams((p) => writeMoments(p, m), { replace: true });
  };

  const settled = useSettledMoments(moments?.a, moments?.b);
  const q = useReplay(settled.a, settled.b);
  const data = q.data;
  // The answer on screen is this system's and these moments' own only when
  // it was asked for them; one kept while the next loads is the earlier
  // choice and says so.
  const current = data !== undefined && moments !== undefined && data.asked.system === sys && data.asked.a === moments.a && data.asked.b === moments.b;
  const settledNow = moments !== undefined && settled.a === moments.a && settled.b === moments.b;

  const [status, setStatus] = useState("");
  const system = new URLSearchParams(global).get(SYSTEM_PARAM) ?? sys;

  /** The absolute link to this A–B over a fixed range, which the page also switches to. */
  const shareUrl = (m: Moments) => {
    const at = serverNow();
    const p = shareParams(params, spec, m, at);
    if (system && !p.has(SYSTEM_PARAM)) p.set(SYSTEM_PARAM, system);
    p.delete(ACCOUNT_PARAM);
    return { url: `${window.location.origin}${pathname}?${p}`, params: p, at };
  };

  const copy = async () => {
    if (!moments) return;
    const s = shareUrl(moments);
    if (spec.kind === "live") setSpec(pauseRange(spec, s.at));
    try {
      await navigator.clipboard.writeText(s.url);
      setStatus("Link to A–B copied; the range is now fixed, so the link shows the same strip.");
    } catch (e) {
      setStatus(`Could not copy the link (${errorText(e)}): ${s.url}`);
    }
  };

  const exportLog = () => {
    if (!current || !data) return;
    const s = shareUrl(moments!);
    const text = replayLog(data, { system, origin: window.location.origin, global: `?${new URLSearchParams([...s.params].filter(([k]) => k !== A_PARAM && k !== B_PARAM))}`, link: s.url });
    download(logFileName(system, data), text);
    setStatus(`Saved ${logFileName(system, data)}.`);
  };

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
                <p key={n}>{n}</p>
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

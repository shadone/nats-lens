import { useId, type ReactNode } from "react";
import { HISTORY_LOADING, HISTORY_LOADING_SHORT, HISTORY_UNAVAILABLE, readState, useLate, type HistoryReadState } from "../lib/historyStatus";
import { useDataHistory } from "../api/queries";
import { usePageHistory, type PageHistory } from "../components/HistoryNote";
import { gapName, stripSegments } from "../lib/historyView";
import { useTimeRange } from "../lib/useTimeRange";
import { PRESETS, type RangeSpec } from "../lib/timeRange";
import { useReportStale } from "../components/StaleContext";
import { isHatched, type HistoryGap, type ServerHistoryGap } from "../api/history";

/** Share the page key for normal charts; wider fixed windows share a bounded
 * domain key (seven-day live charts use the stable 7d key, never a ticking key). */
function chartRange(spec: RangeSpec, start: number, end: number): RangeSpec {
  if (spec.kind === "fixed") {
    return start === spec.from && end === spec.to ? spec : { kind: "fixed", from: start, to: end };
  }
  const span = end - start;
  const preset = PRESETS.find((p) => p.ms === span);
  return preset ? { kind: "live", preset: preset.id } : { kind: "fixed", from: start, to: end };
}

interface Frame { start: number; end: number; width: number; height: number }

/** The page's own history answer covers this chart; otherwise the chart queries its domain. */
function pageGapsApply(h: PageHistory, start: number, end: number): h is PageHistory & { gaps: HistoryGap[] } {
  return !!h.gaps && (h.gapServer === undefined || end - start === h.end - h.start);
}

/** A chart's history: the page's own answer where it covers the chart, else the chart's read of its domain. */
type ChartRead = { h: PageHistory; page: true } | { h: PageHistory; page: false; q: ReturnType<typeof useDataHistory> };

/**
 * Reads a chart's history once the page has one: the page's answer where it
 * covers the chart, else the chart's own query of its domain. A chart with
 * no page history (`without`) reads nothing.
 */
function ChartHistory({ start, end, without = null, reportStale = false, render }: { start: number; end: number; without?: ReactNode; reportStale?: boolean; render: (r: ChartRead) => ReactNode }) {
  const h = usePageHistory();
  if (!h) return <>{without}</>;
  return pageGapsApply(h, start, end) ? <>{render({ h, page: true })}</> : <QueriedHistory h={h} start={start} end={end} reportStale={reportStale} render={render} />;
}
function QueriedHistory({ h, start, end, reportStale, render }: { h: PageHistory; start: number; end: number; reportStale: boolean; render: (r: ChartRead) => ReactNode }) {
  const { spec } = useTimeRange();
  const q = useDataHistory(chartRange(spec, start, end), h.gapServer);
  useReportStale(reportStale ? q : NO_READ);
  return <>{render({ h, page: false, q })}</>;
}
/** Nothing to report for a chart that does not report its read. */
const NO_READ = { data: undefined, error: null, dataUpdatedAt: 0, refetch: () => undefined };

export function HistoryGaps(props: Frame) {
  return <ChartHistory start={props.start} end={props.end} reportStale render={(r) => {
    const { h } = r;
    if (r.page) return <GapMarks {...props} gaps={h.gaps ?? []} observedStart={h.start} observedEnd={h.end} historyStart={h.historyStart} live={h.live} serverGaps={h.serverGaps} />;
    const d = r.q.data;
    return <GapMarks {...props} gaps={d?.gaps ?? []} observedStart={d?.rangeStart} observedEnd={d?.rangeEnd} historyStart={h.historyStart} live={h.live} serverGaps={d?.serverGaps} />;
  }} />;
}

function GapMarks({ start, end, width, height, gaps, live, historyStart, observedStart, observedEnd, serverGaps = [] }: Frame & { serverGaps?: ServerHistoryGap[]; gaps: HistoryGap[]; live: boolean; historyStart?: number; observedStart?: number; observedEnd?: number }) {
  const id = useId();
  // Preserve the answer's observation bounds. An unset end means open at
  // THAT range end, not open through any later chart end or refresh failure.
  const bounded = gaps.flatMap((g) => {
    const from = Math.max(g.start ?? Infinity, observedStart ?? start);
    const to = Math.min(g.end ?? observedEnd ?? end, observedEnd ?? end);
    if (to <= from) return [];
    return [{ ...g, start: from, end: g.end === undefined && to >= end ? undefined : to }];
  });
  // A fixed seven-day chart may begin before the page query. LensInfo's
  // history start covers that unobserved prefix without another request.
  const shown = historyStart !== undefined && historyStart > start
    ? [...bounded.filter((g) => g.kind !== "beforeHistory"), { kind: "beforeHistory" as const, start, end: historyStart, eventId: "", connection: "" }]
    : bounded;
  const segments = stripSegments({ gaps: shown, rangeStart: start, rangeEnd: end }).filter((s) => s.width > 0);
  return <g>
    <defs><pattern id={id} width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(45)"><rect width="6" height="6" className="dh-gap-bg" /><line x1="0" y1="0" x2="0" y2="6" className="dh-gap-line" /></pattern></defs>
    {serverGaps.map((g, i) => {
      const from = Math.max(start, observedStart ?? start, g.start);
      const to = Math.min(end, observedEnd ?? end, g.end);
      if (to <= from || end <= start) return null;
      const x = (from - start) / (end - start) * width;
      const limit = (Math.min(end, observedEnd ?? end) - start) / (end - start) * width;
      return <rect key={`server-${i}`} className="dh-chart-server-gap" x={x} y="0" width={Math.min(limit - x, Math.max(1.5, (to - from) / (end - start) * width))} height={height} fill={`url(#${id})`} role="img" aria-label={g.reason}><title>{g.reason}</title></rect>;
    })}
    {segments.map((s, i) => {
      const name = gapName(s.gap, end, live);
      const limit = s.gap.kind === "beforeHistory" ? width : Math.min(1, ((observedEnd ?? end) - start) / (end - start)) * width;
      return <rect className="dh-chart-gap" key={i} x={s.left / 100 * width} y="0" width={Math.min(limit - s.left / 100 * width, Math.max(1.5, s.width / 100 * width))} height={height} fill={`url(#${id})`} role="img" aria-label={name}><title>{name}</title></rect>;
    })}
  </g>;
}

interface NoticeFacts {
  gaps?: HistoryGap[];
  gapsTotal?: number;
  newestOmittedCollectionGap?: HistoryGap;
}
function OmissionNotice({ facts, start, end, observedStart, observedEnd }: NoticeFactsProps) {
  const omitted = (facts.gapsTotal ?? 0) - (facts.gaps ?? []).filter(isHatched).length;
  const g = facts.newestOmittedCollectionGap;
  // With an exact chart request, its newest omitted gap proves actual omission
  // in the chart. For a retained live answer, only keep that notice while this
  // recorded omitted interval still overlaps; never extend its open end.
  const relevant = g
    ? Math.max(start, observedStart ?? start, g.start ?? Infinity) < Math.min(end, observedEnd ?? end, g.end ?? observedEnd ?? end)
    : start <= (observedStart ?? start) && end >= (observedEnd ?? end);
  return omitted > 0 && relevant ? <p className="dh-chart-notice" role="note" aria-label="Collection gap limit">History response: {omitted} older collection gaps omitted. Hatches may be incomplete.</p> : null;
}
interface NoticeFactsProps { facts: NoticeFacts; start: number; end: number; observedStart?: number; observedEnd?: number }
/** HTML outside the SVG stays legible and accessible at every chart size.
 * The extra observer shares the chart's existing history key and request. */
export function ChartHistoryNotice({ start, end }: { start: number; end: number }) {
  return <ChartHistory start={start} end={end} render={(r) => {
    if (r.page) return <OmissionNotice facts={r.h} start={start} end={end} observedStart={r.h.start} observedEnd={r.h.end} />;
    const d = r.q.data;
    return d ? <OmissionNotice facts={d} start={start} end={end} observedStart={d.rangeStart} observedEnd={d.rangeEnd} /> : null;
  }} />;
}
interface StatusProps {
  start: number;
  end: number;
  children?: ReactNode;
  /**
   * How the text stands in for the children: `keep` keeps their width,
   * hidden under the text, so a wrapping row wraps the same; `slot` is a row
   * shared with other data, with the short text, shown whole or not at all;
   * `plain` replaces them.
   */
  layout?: "keep" | "slot" | "plain";
  /**
   * The children claim what the gaps could disprove, such as "steady": they
   * are replaced while the read fails too. Other rows keep their data then,
   * and the page says once that gaps are not marked.
   */
  claim?: boolean;
}
function StatusText({ state, children, layout = "plain", claim }: Omit<StatusProps, "start" | "end"> & { state: HistoryReadState }) {
  // A shared row keeps its data for the first LOADING_DELAY_MS, so a read the
  // cache answers at once does not flash. A claim gives way at once: it must
  // never say "steady" while the gaps are unknown.
  const loading = state === "loading";
  const late = useLate(loading);
  const failed = state === "failed";
  if (!(loading && (late || claim)) && !(failed && claim)) return <>{children}</>;
  const words = failed ? HISTORY_UNAVAILABLE : layout === "slot" ? HISTORY_LOADING_SHORT : HISTORY_LOADING;
  const text = <span className="dh-chart-status">{words}</span>;
  if (layout === "keep") return <span className="dh-status-in"><span className="dh-status-keep" aria-hidden="true">{children}</span>{text}</span>;
  return layout === "slot" ? <span className="dh-chart-slot">{text}</span> : text;
}
/** The chart's own history read, while it loads, as text in place of
 * `children`: a row the chart already has, so the card keeps its height. SVG
 * text in a stretched chart would scale with the drawing. It is not a live
 * region; the page announces the state once. Where the page's read covers the
 * chart (a server's page), that read's state is said. */
export function ChartHistoryStatus({ start, end, ...rest }: StatusProps) {
  return <ChartHistory start={start} end={end} without={rest.children} render={(r) => <StatusText {...rest} state={r.page ? r.h.gapsState : readState(r.q)} />} />;
}

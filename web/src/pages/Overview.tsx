// Overview: the reference layout. Worst finding and tally, other findings,
// cluster tiles, status timeline with zoom strip, events, streams, servers.
import { useState } from "react";
import type { Finding, Overview as OverviewModel } from "../api/model";
import { useEvents, useFindings, useOverview, useStreams, useTimeline } from "../api/queries";
import { Empty, ErrorBox, Loading, QueryView, StaleBanner } from "../components/common";
import { AllClear, FindingRow, Tally, WorstFinding } from "../components/FindingParts";
import { MuteDialog } from "../components/MuteDialog";
import { EventFeed, FEED_FILTERS, FeedChips, ServersCard, StreamsCard, serversAside } from "../components/OverviewLower";
import { CELL_FILL, StatusTimeline } from "../components/StatusTimeline";
import { Tiles } from "../components/Tiles";
import { ZoomStrip } from "../components/ZoomStrip";
import { usePageTitle } from "../lib/usePageTitle";
import { bucketLabel, rangeLabel } from "../lib/timeRange";
import { zoomWindow } from "../lib/timelineModel";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";
import { useGlobalQuery } from "../lib/useGlobalQuery";

function FindingsTop({ o, onMute }: { o: OverviewModel; onMute: (f: Finding) => void }) {
  const { spec } = useTimeRange();
  const query = useGlobalQuery();
  const list = useFindings(spec, { includeMuted: true, pageSize: 50 });
  const end = o.range?.end ?? Date.now();
  const others = (list.data?.items ?? []).filter((f) => f.id !== o.worst?.id);
  const checksRun = o.tally.resolved24h > 0 || o.tally.muted > 0;
  return (
    <>
      <section className="find" aria-label="Findings now">
        {o.worst ? <WorstFinding f={o.worst} rangeStart={o.range?.start} end={end} onMute={onMute} queryString={query} /> : <AllClear resolved={o.tally.resolved24h} checksRun={checksRun} />}
        <Tally t={o.tally} />
      </section>
      <StaleBanner q={list} />
      {list.error && !list.data ? (
        <div className="others">
          <ErrorBox error={list.error} retry={() => list.refetch()} what="findings" />
        </div>
      ) : (
        others.length > 0 && (
          <section className="card others" aria-label="Other findings">
            {others.map((f) => (
              <FindingRow key={f.id} f={f} end={end} rangeStart={o.range?.start} />
            ))}
          </section>
        )
      )}
    </>
  );
}

function TimelineCard({ end }: { end: number }) {
  const { spec } = useTimeRange();
  const q = useTimeline(spec);
  const [asTable, setAsTable] = useState(false);
  const tl = q.data;
  const win = tl ? zoomWindow(tl.windows) : undefined;
  const used = new Set(tl?.rows.flatMap((r) => r.cells) ?? []);
  return (
    <section className="sec">
      <div className="card tl">
        <div className="sec-h">
          <h2>Status timeline</h2>
          <span className="aside">
            Worst state per check, {tl ? bucketLabel(tl.bucketMs) : "…"} buckets. Checks failing now are listed first.
            <span className="scroll-hint"> Scroll the cells sideways for earlier hours; it opens at now.</span>
            {tl?.natsTruncated && <span className="trunc"> The range holds more NATS events than the lanes show; the newest 1000 are drawn.</span>}
            {tl?.findingTruncated && (
              <span className="trunc"> The range holds more finding transitions than the lanes show; the newest 1000 are drawn.</span>
            )}
            {tl?.kubernetesTruncated && (
              <span className="trunc"> The range holds more Kubernetes events than the lane shows; the newest 1000 are drawn.</span>
            )}
          </span>
          <div className="right legend">
            {(["ok", "info", "warning", "critical", "muted", "nodata"] as const)
              .filter((l) => l === "ok" || l === "info" || l === "warning" || l === "critical" || used.has(l))
              .map((l) => (
                <span key={l}>
                  <i style={{ background: CELL_FILL[l] }} />
                  {l === "nodata" ? "no data" : l}
                </span>
              ))}
            <button type="button" className="chip" aria-pressed={asTable} onClick={() => setAsTable((t) => !t)}>
              {asTable ? "Show chart" : "Show as table"}
            </button>
          </div>
        </div>
        <QueryView q={q} what="the status timeline">
          {(tl) =>
            tl.rows.length === 0 && tl.kubernetes.length === 0 ? (
              <Empty>No check has reported in this range. The timeline fills in once checks run after each epoch.</Empty>
            ) : (
              <StatusTimeline tl={tl} end={end} live={spec.kind === "live"} asTable={asTable} />
            )
          }
        </QueryView>
        {tl && win && !asTable && <ZoomStrip tl={tl} win={win} />}
      </div>
    </section>
  );
}

function Lower({ o }: { o: OverviewModel }) {
  const { spec } = useTimeRange();
  const [filter, setFilter] = useState("all");
  const sources = FEED_FILTERS.find((f) => f.id === filter)!.sources;
  const ev = useEvents(spec, { sources, pageSize: 40 });
  const st = useStreams(spec);
  const end = o.range?.end ?? Date.now();
  const causeId = o.worst?.cause?.id;
  return (
    <section className="sec lower">
      <div className="col">
        <div className="sec-h">
          <h2>Events</h2>
          <span className="aside">NATS advisories, Kubernetes events and finding transitions</span>
          <FeedChips value={filter} onChange={setFilter} />
        </div>
        <QueryView q={ev} what="events">
          {(page) => (
            <EventFeed
              key={`${filter}:${causeId}`}
              events={page.items}
              total={page.totalCount}
              end={end}
              initialOpen={page.items.find((e) => e.id === causeId || e.raised.some((f) => f.id === o.worst?.id))?.id}
              moreHref={`/events${filter === "all" ? "" : `?src=${filter}`}`}
              empty={<Empty>No events in this range.</Empty>}
            />
          )}
        </QueryView>
      </div>
      <div className="col">
        <div className="sec-h">
          <h2>Streams</h2>
          {st.data && (
            <span className="aside">
              {st.data.streams.length} streams
              {st.data.streams.length > 0 && new Set(st.data.streams.map((s) => s.replicas)).size === 1 && st.data.streams[0].replicas !== undefined ? `, all R${st.data.streams[0].replicas}` : ""}. Worst first.
            </span>
          )}
        </div>
        <QueryView q={st} what="streams">
          {(d) => (d.streams.length ? <StreamsCard streams={d.streams} totals={d.totals} /> : <Empty>No streams in the newest epoch of this range.</Empty>)}
        </QueryView>
        <div className="sec-h" style={{ marginTop: 22 }}>
          <h2>Servers</h2>
          <span className="aside">{serversAside(o.servers, o.windows)}</span>
        </div>
        {o.servers.length ? <ServersCard servers={o.servers} /> : <Empty>No server reported in this range.</Empty>}
      </div>
    </section>
  );
}

export function Overview() {
  usePageTitle("Overview");
  const { spec } = useTimeRange();
  const q = useOverview(spec);
  const fallback = useResolvedRange(spec);
  const [muting, setMuting] = useState<Finding>();
  if (!q.data) return q.error ? <ErrorBox error={q.error} retry={() => q.refetch()} what="the overview" /> : <Loading what="the overview" />;
  const o = q.data;
  const start = o.range?.start ?? fallback.start;
  const end = o.range?.end ?? fallback.end;
  const shaded = o.windows.length ? ` Shaded: ${[...new Set(o.windows.map((w) => w.label))].join(", ")}.` : "";
  const span = rangeLabel(spec).replace(/^Last /, "");
  return (
    <>
      <StaleBanner q={q} />
      <FindingsTop o={o} onMute={setMuting} />
      <section className="sec">
        <div className="sec-h">
          <h2>Cluster now</h2>
          <span className="aside">
            Current value, {spec.kind === "live" ? `last ${span}` : "the range"}, and the {spec.kind === "live" ? span : "range's"} min to max range.{shaded}
          </span>
        </div>
        {o.tiles.length ? <Tiles tiles={o.tiles} windows={o.windows} start={start} end={end} /> : <Empty>No epoch stored in this range yet.</Empty>}
      </section>
      <TimelineCard end={end} />
      <Lower o={o} />
      <MuteDialog finding={muting} onClose={() => setMuting(undefined)} />
    </>
  );
}

// Overview: the cockpit. A status line, the system's numbers, the findings
// grouped by cause, the trends, what happened in lanes, the busiest
// consumers and the streams with their placement.
import { useState } from "react";
import type { Finding } from "../api/model";
import { CURRENT_FINDINGS_LIMIT, useBusiestConsumers, useCurrentFindings, useStreamPlacement } from "../api/overview";
import { useOverview, useTimeline } from "../api/queries";
import { BusiestConsumers } from "../components/BusiestConsumers";
import { CauseGroups } from "../components/CauseGroups";
import { Empty, ErrorBox, Loading, QueryView, RLink, StaleBanner } from "../components/common";
import { MuteDialog } from "../components/MuteDialog";
import { StatusLine } from "../components/StatusLine";
import { StoryLanes } from "../components/StoryLanes";
import { StreamPlacementCard } from "../components/StreamPlacementCard";
import { OverviewTiles } from "../components/OverviewTiles";
import { Trends } from "../components/Trends";
import { groupCauses } from "../lib/causeGroups";
import { usePageTitle } from "../lib/usePageTitle";
import { useAccountScope } from "../lib/useSystem";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";

function Causes({ onMute }: { onMute: (f: Finding) => void }) {
  const { spec } = useTimeRange();
  const q = useCurrentFindings(spec);
  if (!q.data) return q.error ? <ErrorBox error={q.error} retry={() => q.refetch()} what="findings" /> : null;
  const { groups, muted } = groupCauses(q.data.items);
  const cut = q.data.totalCount > q.data.items.length;
  return (
    <>
      <StaleBanner q={q} />
      <CauseGroups groups={groups} muted={muted} onMute={onMute} />
      {cut && (
        <p className="muted-note">
          The cards read the first {CURRENT_FINDINGS_LIMIT} of {q.data.totalCount} findings.
        </p>
      )}
    </>
  );
}

function Story({ end }: { end: number }) {
  const { spec } = useTimeRange();
  const q = useTimeline(spec);
  const tl = q.data;
  return (
    <section className="sec">
      <div className="card story-card">
        <div className="sec-h">
          <h2>Timeline</h2>
          <span className="aside">
            {tl?.natsTruncated && <span className="trunc">The range holds more NATS events than the lanes show; the newest 1000 are drawn. </span>}
            {tl?.findingTruncated && <span className="trunc">The range holds more finding transitions than the lanes show; the newest 1000 are drawn. </span>}
            {tl?.kubernetesTruncated && <span className="trunc">The range holds more Kubernetes events than the lane shows; the newest 1000 are drawn.</span>}
          </span>
        </div>
        <QueryView q={q} what="the timeline">
          {(t) => <StoryLanes tl={t} end={end} live={spec.kind === "live"} />}
        </QueryView>
      </div>
    </section>
  );
}

function Lower({ start, end }: { start: number; end: number }) {
  const { spec } = useTimeRange();
  const [account] = useAccountScope();
  const bc = useBusiestConsumers(spec, account);
  const st = useStreamPlacement(spec, account);
  return (
    <div className="ov-two">
      <section className="card ov-panel" aria-labelledby="ov-bc">
        <div className="sec-h">
          <h2 id="ov-bc">Busiest consumers</h2>
          <span className="right">
            <RLink to="/consumers">All consumers</RLink>
          </span>
        </div>
        <QueryView q={bc} what="the busiest consumers">
          {(cs) => (cs.length ? <BusiestConsumers consumers={cs} spec={spec} start={start} end={end} /> : <Empty>No consumer in the newest collection of this range.</Empty>)}
        </QueryView>
        <p className="muted-note">Delivery rate over the 15 minutes before the range's end, from each consumer's leader.</p>
      </section>
      <section className="card ov-panel" aria-labelledby="ov-st">
        <div className="sec-h">
          <h2 id="ov-st">Streams</h2>
          <span className="right">
            <RLink to="/streams">All streams</RLink>
          </span>
        </div>
        <QueryView q={st} what="streams">
          {(d) => (d.streams.length ? <StreamPlacementCard streams={d.streams} count={d.count} /> : <Empty>No streams in the newest collection of this range.</Empty>)}
        </QueryView>
      </section>
    </div>
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
  return (
    <>
      <StaleBanner q={q} />
      <StatusLine tally={o.tally} freshness={o.freshness} live={spec.kind === "live"} now={Date.now()} />
      <section className="sec" aria-label="Health">
        {o.tiles.length ? <OverviewTiles tiles={o.tiles} windows={o.windows} start={start} end={end} /> : <Empty>No collection stored in this range yet.</Empty>}
      </section>
      <Causes onMute={setMuting} />
      <section className="sec">
        <div className="sec-h">
          <h2>Trends</h2>
        </div>
        <Trends spec={spec} start={start} end={end} />
      </section>
      <Story end={end} />
      <Lower start={start} end={end} />
      <MuteDialog finding={muting} onClose={() => setMuting(undefined)} />
    </>
  );
}

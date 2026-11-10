// Overview: the cockpit. A status line, the system's numbers, the findings
// grouped by cause, the trends, what happened in lanes, the busiest
// consumers and the streams with their placement.
import { useState } from "react";
import type { Finding } from "../api/model";
import { ASSET_PLACEMENT_LIMIT, CURRENT_FINDINGS_LIMIT, useAssetPlacement, useBusiestConsumers, useCurrentFindings, useStreamPlacement } from "../api/overview";
import { AcceptDialog, type AcceptTarget } from "../components/AcceptDialog";
import { useDataHistory, useLensInfo, useOverview, useTimeline } from "../api/queries";
import { BusiestConsumers } from "../components/BusiestConsumers";
import { CauseGroups } from "../components/CauseGroups";
import { Empty, ErrorBox, Loading, QueryView, RLink, StaleBanner } from "../components/common";
import { MuteDialog } from "../components/MuteDialog";
import { StatusLine } from "../components/StatusLine";
import { WindowReplayEntries } from "../components/ReplayEntry";
import { StoryLanes } from "../components/StoryLanes";
import { StreamPlacementCard } from "../components/StreamPlacementCard";
import { OverviewTiles } from "../components/OverviewTiles";
import { Trends } from "../components/Trends";
import { evidenceEntities } from "../lib/causeEvidence";
import { groupCauses } from "../lib/causeGroups";
import { serverNow } from "../lib/clock";
import { usePageTitle } from "../lib/usePageTitle";
import { useAccountScope, useSystem } from "../lib/useSystem";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";

/** The finding an acceptance from a card covers. */
function acceptTarget(f: Finding): AcceptTarget {
  return { findingId: f.id, checkCode: f.checkCode, checkName: f.checkName, entity: f.entity!, growthReview: !!f.growthReview };
}

function Causes({ onMute }: { onMute: (f: Finding) => void }) {
  const { spec } = useTimeRange();
  const system = useSystem();
  const [account, setAccount] = useAccountScope();
  const q = useCurrentFindings(spec, account);
  const [accepting, setAccepting] = useState<AcceptTarget>();
  const { groups, muted } = groupCauses(q.data?.items ?? []);
  const asked = evidenceEntities(groups, ASSET_PLACEMENT_LIMIT);
  const p = useAssetPlacement(spec, asked);
  if (!q.data) return q.error ? <ErrorBox error={q.error} retry={() => q.refetch()} what="findings" /> : null;
  const cut = q.data.totalCount > q.data.items.length;
  const others = q.data.otherAccountCount ?? 0;
  const card = { read: { data: p.data, loading: p.isPending, failed: p.isError, asked: new Set(asked.map((e) => `${e.kind}:${e.key}`)) }, system, scoped: account !== "", onAccept: (f: Finding) => setAccepting(acceptTarget(f)) };
  return (
    <>
      <StaleBanner q={q} />
      <CauseGroups groups={groups} muted={muted} onMute={onMute} card={card} />
      {cut && (
        <p className="muted-note">
          The cards read the first {CURRENT_FINDINGS_LIMIT} of {q.data.totalCount} findings.
        </p>
      )}
      {account && others > 0 && (
        <p className="muted-note">
          {others === 1 ? "1 finding of another account is" : `${others} findings of other accounts are`} not shown.{" "}
          <button type="button" className="linkish" onClick={() => setAccount("")}>
            Show all accounts
          </button>
        </p>
      )}
      <AcceptDialog target={accepting} onClose={() => setAccepting(undefined)} />
    </>
  );
}

function Story({ end }: { end: number }) {
  const { spec } = useTimeRange();
  const q = useTimeline(spec, undefined, true);
  const tl = q.data;
  const info = useLensInfo().data;
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
          {(t) => <><StoryLanes tl={t} end={end} live={spec.kind === "live"} /><WindowReplayEntries windows={t.windows} now={serverNow()} minGapMs={info?.collectIntervalMs || 30_000} /></>}
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
        <p className="muted-note">Delivery-sequence rate in the newest comparable leader run within 15 minutes before the range’s end; not unique messages.</p>
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
  // The tiles decide on their charts from the range's gaps: asked with the
  // overview, under the charts' key, so they load together.
  useDataHistory(spec);
  const fallback = useResolvedRange(spec);
  const [muting, setMuting] = useState<Finding>();
  const [account] = useAccountScope();
  if (!q.data) return q.error ? <ErrorBox error={q.error} retry={() => q.refetch()} what="the overview" /> : <Loading what="the overview" />;
  const o = q.data;
  const start = fallback.start;
  const end = fallback.end;
  return (
    <>
      <StaleBanner q={q} />
      <div className="ov-phone-order">
      <StatusLine tally={o.tally} freshness={o.freshness} live={spec.kind === "live"} now={serverNow()} />
      <section className="sec ov-health" aria-label="Health">
        <div className="sec-h"><h2>System summary</h2></div>
        <p className="muted-note">Latest available counts and reports, with history over the selected range.</p>
        {account && <p className="muted-note">The summary, trends and timeline cover all accounts; the findings, busiest consumers and streams follow the account.</p>}
        {o.tiles.length ? <OverviewTiles tiles={o.tiles} windows={o.windows} start={start} end={end} /> : <Empty>No collection stored in this range yet.</Empty>}
      </section>
      <div className="ov-causes"><Causes onMute={setMuting} /></div>
      <div className="ov-rest">
      <section className="sec">
        <div className="sec-h">
          <h2>Detailed trends</h2>
        </div>
        <p className="muted-note">Traffic rates, event totals, server spread, and replica counts over the selected range. Hover or touch a chart to inspect a bucket, or focus it and use arrow keys.</p>
        <Trends spec={spec} start={start} end={end} />
      </section>
      <Story end={end} />
      <Lower start={start} end={end} />
      </div>
      </div>
      <MuteDialog finding={muting} onClose={() => setMuting(undefined)} />
    </>
  );
}

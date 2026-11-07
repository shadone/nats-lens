import { HistoryLink, usePageHistory } from "../components/HistoryNote";
import { historySpan, spanText } from "../lib/historyView";
// Applications: the client connections of the range's newest collection
// grouped by the application in their name, with three summary cards,
// a table that sorts by any column (kept in the address as ?sort=) and a
// footnote. ?tooling=1 lists the tooling connections too, as on the
// Connections page.
import { useSearchParams } from "react-router";
import type { Applications as Apps } from "../api/applications";
import { useApplications } from "../api/queries";
import { useAccounts } from "../api/systems";
import { AppCards } from "../components/AppCards";
import { AppTable } from "../components/AppTable";
import { Empty, QueryView } from "../components/common";
import { APPS_FOOTNOTE, CAPTURE_GAPS, CONNZ_CUT, emptyText, NO_EVENTS, partialText, subtitleText, TOOLING_PARAM, toolingHiddenText, trafficNote } from "../lib/appsView";
import { usePageTitle } from "../lib/usePageTitle";
import { useAccountScope } from "../lib/useSystem";
import { useNow, useResolvedRange, useTimeRange } from "../lib/useTimeRange";
import { ReplayContextLink } from "../components/ReplayEntry";
import { replayEntry } from "../lib/replayEntry";
import { accountLabeller } from "../lib/view";

function Body({ d, includeTooling, scope, names }: { d: Apps; includeTooling: boolean; scope: string; names: Map<string, string> }) {
  const h = usePageHistory();
  const [params] = useSearchParams();
  const selectedApp = params.get("app");
  const rows = selectedApp ? d.applications.filter(a => a.name === selectedApp) : d.applications;
  const covered = h && historySpan(h.historyStart, h.end - 86_400_000, h.end);
  const accounts = d.applications.flatMap((a) => a.accounts);
  const named = accounts.map((x) => ({ account: x.account, accountName: x.accountName || names.get(x.account) || "" }));
  const label = accountLabeller(named);
  const nameOf = (id: string) => named.find((x) => x.account === id && x.accountName)?.accountName || names.get(id) || undefined;
  const showAccount = scope === "" && new Set(accounts.map((x) => x.account)).size > 1;
  const note = trafficNote(d.applications);
  return (
    <>
      {covered !== undefined && covered < 86_400_000 && <p className="ap-notice" role="note"><HistoryLink>{spanText(covered)} of the last 24 h have history; hours before nats-lens started are missing.</HistoryLink></p>}
      {/* The page is partial exactly when a server is missing; the notice names them. */}
      {d.missing.length > 0 && (
        <p className="ap-notice ap-notice-warn" role="note">
          <b>Partial collection.</b> {partialText(d.missing)}
        </p>
      )}
      {d.eventsCaptured && d.captureGaps && d.applications.length > 0 && (
        <p className="ap-notice" role="note">
          <b>Event capture stopped for part of these 24 hours.</b> {CAPTURE_GAPS}
        </p>
      )}
      {d.connzCut && (
        <p className="ap-notice ap-notice-warn" role="note">
          <b>Connection list cut.</b> {CONNZ_CUT}
        </p>
      )}
      {!d.eventsCaptured && d.applications.length > 0 && (
        <p className="ap-notice" role="note">
          <b>Events not captured.</b> {NO_EVENTS}
        </p>
      )}
      {d.toolingHidden > 0 && d.applications.length > 0 && <p className="muted-note">{toolingHiddenText(d.toolingHidden)}</p>}
      {d.applications.length === 0 ? (
        <Empty>
          <span className="ap-wrap">{emptyText(d, includeTooling, scope ? label(scope, nameOf(scope)) : "")}</span>
        </Empty>
      ) : (
        <>
          <AppCards d={d} />
          {selectedApp && <p className="ap-notice" role="note">Table filtered to application {selectedApp}. Summary cards cover the selected account scope. {rows.length === 0 && "Application not listed in this collection; it may be absent or beyond the 500-application limit."}</p>}
          {note && (
            <p className="ap-notice" role="note">
              {note}
            </p>
          )}
          <AppTable rows={rows} showAccount={showAccount} label={(id, name) => label(id, name || nameOf(id))} atLeast={d.eventsCaptured && d.captureGaps} />
        </>
      )}
      <div className="ap-foot">
        {APPS_FOOTNOTE.map((l) => (
          <p key={l}>{l}</p>
        ))}
      </div>
    </>
  );
}

export function Applications() {
  usePageTitle("Applications");
  const [params, setParams] = useSearchParams();
  const { spec } = useTimeRange();
  const [account] = useAccountScope();
  const range = useResolvedRange(spec);
  const now = useNow(30_000);
  const context = replayEntry({ a: Math.max(range.start, range.end - 86_400_000), b: range.end }, now, 30_000);
  const tooling = params.get(TOOLING_PARAM) === "1";
  const q = useApplications(spec, account, tooling);
  // The header's account list names an account the response leaves unnamed.
  const listed = useAccounts(spec).data;
  const names = new Map((listed ?? []).filter((a) => a.name).map((a) => [a.account, a.name]));
  const setTooling = (on: boolean) =>
    setParams(
      (p) => {
        const n = new URLSearchParams(p);
        if (on) n.set(TOOLING_PARAM, "1");
        else n.delete(TOOLING_PARAM);
        return n;
      },
      { replace: true },
    );
  return (
    <>
      <div className="page-h">
        <span className="aside">{subtitleText(q.data)}</span>
      </div>
      <div className="filters">
        <label className="check">
          <input type="checkbox" checked={tooling} onChange={(e) => setTooling(e.target.checked)} /> Include tooling
        </label>
      </div>
      {context && <div className="replay-entry">
        <ReplayContextLink to={context.to} label="Replay application context" />
        <p>Compare observed client-library sets alongside system-wide reconnects, findings and server upgrades in this window (at most 24 hours). Library changes do not prove an application deployment. Replay covers all accounts. {context.text}</p>
      </div>}
      <QueryView q={q} what="applications">
        {(d) => (d.collected ? <Body d={d} includeTooling={tooling} scope={account} names={names} /> : <Empty>No collection in this range.</Empty>)}
      </QueryView>
    </>
  );
}

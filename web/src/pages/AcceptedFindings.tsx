// Accepted findings: the acceptances as by design, the soonest review date
// first, with who accepted each, why, the growth recorded at acceptance and
// a way to end one; the ended ones behind a toggle. Follows the account
// scope as the Findings page does.
import { useState } from "react";
import { useCheckDefinitions } from "../api/checks";
import type { Mute } from "../api/model";
import { useAcceptances } from "../api/queries";
import { Empty, QueryView, RLink } from "../components/common";
import { EntityLine } from "../components/FindingParts";
import { UnmuteDialog } from "../components/MuteDialog";
import { Pager, usePager } from "../components/Pager";
import { AcceptedTag, Sev } from "../components/Severity";
import { serverNow } from "../lib/clock";
import { fmtDuration, fmtGrowth, isoDate, isoMinute } from "../lib/format";
import { endedByLens, growthAtAcceptance, muteActive, sentence } from "../lib/muteView";
import { usePageTitle } from "../lib/usePageTitle";
import { useAccountScope, useSystem } from "../lib/useSystem";
import { accountLabeller, entityName, findingPath } from "../lib/view";

/** The finding an acceptance covers: its check code and entity key. */
function findingIdOf(m: Mute): string {
  return `${m.checkCode}:${m.entity?.key ?? ""}`;
}

function Row({ m, now, name, onEnd, label }: { m: Mute; now: number; name?: string; onEnd: (m: Mute) => void; label: (id: string, name?: string) => string }) {
  const active = muteActive(m, now);
  const lens = endedByLens(m);
  const growth = growthAtAcceptance(m, fmtGrowth);
  return (
    <div className="frow mute">
      <span>{active ? <AcceptedTag /> : <Sev level="muted" label={m.unmutedAt !== undefined ? "Ended" : "Expired"} />}</span>
      <span className="code">
        <RLink to={findingPath(findingIdOf(m))}>{m.checkCode}</RLink>
      </span>
      <span>
        {name && (
          <>
            <span className="t">{name}</span>
            <br />
          </>
        )}
        <span className="e">
          <EntityLine e={m.entity} small label={label} />
        </span>
      </span>
      <span className="s">
        {m.expiresAt !== undefined &&
          (active ? (
            <>
              review on
              <br />
              {isoDate(m.expiresAt)}
              <br />
              in {fmtDuration(m.expiresAt - now)}
            </>
          ) : (
            `ended ${isoMinute(m.unmutedAt ?? m.expiresAt)}`
          ))}
        {active && (
          <>
            <br />
            <button type="button" className="btn sm" onClick={() => onEnd(m)} aria-label={`End acceptance of ${m.checkCode} for ${entityName(m.entity, label)}…`}>
              End acceptance…
            </button>
          </>
        )}
      </span>
      <span className="mute-why">
        <b>Accepted by {m.mutedBy || "unknown"}</b>
        {m.createdAt !== undefined && ` on ${isoMinute(m.createdAt)}`}: {sentence(m.reason)}
        {m.expiresAt !== undefined && ` Review on ${isoDate(m.expiresAt)}.`}
        {growth && ` ${growth}`}
        {lens ? ` ${lens}` : m.unmutedAt !== undefined && ` Ended ${isoMinute(m.unmutedAt)} by ${m.unmutedBy || "unknown"}.`}
      </span>
    </div>
  );
}

export function AcceptedFindings() {
  usePageTitle("Accepted findings");
  const [ended, setEnded] = useState(false);
  const [ending, setEnding] = useState<Mute>();
  const [account, setAccount] = useAccountScope();
  const pager = usePager([useSystem(), String(ended), account].join("|"));
  const q = useAcceptances(ended, account, pager.token);
  const defs = useCheckDefinitions();
  const names = new Map((defs.data?.definitions ?? []).map((d) => [d.code, d.name]));
  const now = serverNow();
  return (
    <>
      <nav className="crumbs" aria-label="Breadcrumb">
        <RLink to="/findings">Findings</RLink>
        <span aria-hidden="true">/</span>
        <span>Accepted findings</span>
      </nav>
      <div className="page-h">
        <span className="aside">
          Findings accepted as by design, the soonest review first. An acceptance hides its finding until the review date; nats-lens ends one early when its check reviews growth and the stream's growth changes.
        </span>
        <span className="right">
          <label className="check chip">
            <input type="checkbox" checked={ended} onChange={(e) => setEnded(e.target.checked)} /> Include ended
          </label>
        </span>
      </div>
      <QueryView q={q} what="accepted findings">
        {(page) => {
          const label = accountLabeller(page.items.flatMap((m) => (m.entity?.kind === "account" ? [m.entity] : [])));
          return (
            <div className="card others mutes-list">
              {page.items.length ? (
                page.items.map((m) => <Row key={m.id} m={m} now={now} name={names.get(m.checkCode)} onEnd={setEnding} label={label} />)
              ) : (
                <Empty>{ended ? "No finding has been accepted." : "No acceptance in force."}</Empty>
              )}
              {(page.nextPageToken || pager.page > 1) && (
                <Pager page={pager.page} next={page.nextPageToken} onNext={() => pager.next(page.nextPageToken)} onPrev={pager.prev} total={page.totalCount} shown={page.items.length} />
              )}
              {account && page.otherAccountCount > 0 && (
                <p className="muted-note">
                  {page.otherAccountCount === 1 ? "1 acceptance of another account is" : `${page.otherAccountCount} acceptances of other accounts are`} not shown.{" "}
                  <button type="button" className="linkish" onClick={() => setAccount("")}>
                    Show all accounts
                  </button>
                </p>
              )}
            </div>
          );
        }}
      </QueryView>
      <UnmuteDialog mute={ending} onClose={() => setEnding(undefined)} />
    </>
  );
}

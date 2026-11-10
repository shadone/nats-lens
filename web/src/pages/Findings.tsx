// Findings: every finding in the range by severity and state, worst first,
// and the mutes with who set them, why and until when.
import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useLocation, useSearchParams } from "react-router";
import type { Entity, FindingState, Mute, SeverityLevel } from "../api/model";
import { useFindings, useMutes } from "../api/queries";
import { Empty, QueryView, Section } from "../components/common";
import { Pager, usePager } from "../components/Pager";
import { EntityLine, FindingRow } from "../components/FindingParts";
import { UnmuteDialog } from "../components/MuteDialog";
import { AcceptedTag, Sev } from "../components/Severity";
import { serverNow } from "../lib/clock";
import { fmtDuration, isoMinute } from "../lib/format";
import { accountLabeller, entityName, LEVEL_LABEL } from "../lib/view";
import { endedByLens, muteActive, muteBy, reviewOn, sentence } from "../lib/muteView";
import { usePageTitle } from "../lib/usePageTitle";
import { useAccountScope, useSystem } from "../lib/useSystem";
import { rangeKey } from "../lib/timeRange";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";

const SEVERITIES: SeverityLevel[] = ["critical", "warning", "info"];

const STATES: { id: string; label: string; states: FindingState[] }[] = [
  { id: "open", label: "Firing", states: [] },
  { id: "flapping", label: "Flapping", states: ["flapping"] },
  { id: "resolved", label: "Resolved", states: ["resolved"] },
  { id: "all", label: "All", states: ["firing", "changed", "flapping", "resolved"] },
];

/** The lift button's name, with the check and what it covers: "End acceptance of stream-005 for order-archive…". */
type Label = (id: string, name?: string) => string;

/** The accounts of the listed entities, labelled apart where two share a name. */
function labelOf(es: (Entity | undefined)[]): Label {
  return accountLabeller(accountsOf(es));
}

function accountsOf(es: (Entity | undefined)[]): Entity[] {
  return es.filter((e): e is Entity => e?.kind === "account");
}

function liftName(m: Mute, label?: Label): string {
  return `${m.accepted ? "End acceptance" : "Lift mute"} of ${m.checkCode} for ${entityName(m.entity, label) || "every entity"}…`;
}

function MuteRow({ m, now, onLift, label }: { m: Mute; now: number; onLift: (m: Mute) => void; label?: Label }) {
  const active = muteActive(m, now);
  const ended = endedByLens(m);
  return (
    <div className="frow mute">
      <span>
        {active && m.accepted ? <AcceptedTag /> : <Sev level="muted" label={active ? "Muted" : m.unmutedAt !== undefined ? (m.accepted ? "Ended" : "Lifted") : "Expired"} />}
      </span>
      <span className="code">{m.checkCode}</span>
      <span>
        <span className="e">{m.entity ? <EntityLine e={m.entity} small label={label} /> : "every entity"}</span>
      </span>
      <span className="s">
        {m.expiresAt !== undefined && (active ? `${m.accepted ? "review" : "ends"} in ${fmtDuration(m.expiresAt - now)}` : `ended ${isoMinute(m.unmutedAt ?? m.expiresAt)}`)}
        {active && (
          <>
            <br />
            <button type="button" className="btn" onClick={() => onLift(m)} aria-label={liftName(m, label)}>
              {m.accepted ? "End…" : "Lift…"}
            </button>
          </>
        )}
      </span>
      <span className="mute-why">
        <b>{muteBy(m)}</b>
        {m.createdAt !== undefined && ` on ${isoMinute(m.createdAt)}`}: {m.accepted ? sentence(m.reason) : m.reason}
        {m.expiresAt !== undefined && (m.accepted ? ` ${reviewOn(m)}` : ` Expires ${isoMinute(m.expiresAt)} UTC.`)}
        {ended ? ` ${ended}` : m.unmutedAt !== undefined && ` ${m.accepted ? "Ended" : "Lifted"} ${isoMinute(m.unmutedAt)} by ${m.unmutedBy || "unknown"}.`}
      </span>
    </div>
  );
}

/**
 * The mutes list. others are the accounts the findings list shows, and
 * onAccounts receives the mutes' accounts, so that one labeller over both
 * lists names an account the same in each.
 */
function Mutes({ others, onAccounts }: { others: Entity[]; onAccounts: (es: Entity[]) => void }) {
  const [inactive, setInactive] = useState(false);
  const [lifting, setLifting] = useState<Mute>();
  const pager = usePager([useSystem(), String(inactive)].join("|"));
  const q = useMutes(inactive, pager.token);
  const now = serverNow();
  const own = useMemo(() => accountsOf(q.data?.items.map((m) => m.entity) ?? []), [q.data]);
  useEffect(() => onAccounts(own), [own, onAccounts]);
  return (
    <Section
      title="Mutes"
      aside="A muted or accepted finding still records transitions and never alerts. Every mute ends; an acceptance at its review date."
      right={
        <label className="check chip">
          <input type="checkbox" checked={inactive} onChange={(e) => setInactive(e.target.checked)} /> Include ended
        </label>
      }
    >
      <QueryView q={q} what="mutes">
        {(page) => {
          const label = labelOf([...page.items.map((x) => x.entity), ...others]);
          return page.items.length ? (
            <div className="card others mutes-list">
              {page.items.map((m) => (
                <MuteRow key={m.id} m={m} now={now} onLift={setLifting} label={label} />
              ))}
              {(page.nextPageToken || pager.page > 1) && (
                <Pager page={pager.page} next={page.nextPageToken} onNext={() => pager.next(page.nextPageToken)} onPrev={pager.prev} total={page.totalCount} shown={page.items.length} />
              )}
            </div>
          ) : (
            <Empty>No mutes{inactive ? "" : " in effect"}.</Empty>
          );
        }}
      </QueryView>
      <UnmuteDialog mute={lifting} onClose={() => setLifting(undefined)} />
    </Section>
  );
}

export function Findings() {
  const labelLookup = (useLocation().state as { labelLookup?: { code: string; key: string } } | null)?.labelLookup;
  usePageTitle("Findings");
  const [params, setParams] = useSearchParams();
  const { spec } = useTimeRange();
  const { start, end } = useResolvedRange(spec);
  const sev = (params.get("sev") ?? "").split(",").filter((s): s is SeverityLevel => SEVERITIES.includes(s as SeverityLevel));
  const stateId = STATES.find((s) => s.id === params.get("state"))?.id ?? "open";
  const muted = params.get("muted") !== "0";
  const check = params.get("check") ?? "";
  const [checkText, setCheckText] = useState(check);
  const [muteAccounts, setMuteAccounts] = useState<Entity[]>([]);
  const [account, setAccount] = useAccountScope();
  const pager = usePager([useSystem(), sev.join(), stateId, muted, check, account, rangeKey(spec)].join("|"));
  const q = useFindings(spec, {
    account,
    severities: sev,
    states: STATES.find((s) => s.id === stateId)!.states,
    includeMuted: muted,
    checkCode: check,
    pageSize: 50,
    pageToken: pager.token,
  });

  const set = (k: string, v: string) =>
    setParams((p) => {
      const n = new URLSearchParams(p);
      if (v) n.set(k, v);
      else n.delete(k);
      return n;
    });
  const toggleSev = (s: SeverityLevel) => set("sev", (sev.includes(s) ? sev.filter((x) => x !== s) : [...sev, s]).join(","));
  const applyCheck = (e: FormEvent) => {
    e.preventDefault();
    set("check", checkText.trim());
  };

  return (
    <>
      {labelLookup && <p className="oc-lookup" role="status">No finding of {labelLookup.code} for {labelLookup.key}; it may have resolved and been swept.</p>}
      <div className="page-h">
        <span className="aside">A finding is one failing check for one entity. Unmuted first, then by severity, expiry warnings after the other warnings, then the longest firing.</span>
      </div>
      <div className="filters">
        <div className="grp" role="group" aria-label="Severity">
          <span className="lbl">Severity</span>
          <button type="button" className={`chip${sev.length === 0 ? " on" : ""}`} aria-pressed={sev.length === 0} onClick={() => set("sev", "")}>
            All
          </button>
          {SEVERITIES.map((s) => (
            <button key={s} type="button" className={`chip${sev.includes(s) ? " on" : ""}`} aria-pressed={sev.includes(s)} onClick={() => toggleSev(s)}>
              {LEVEL_LABEL[s]}
            </button>
          ))}
        </div>
        <div className="grp" role="group" aria-label="State">
          <span className="lbl">State</span>
          {STATES.map((s) => (
            <button key={s.id} type="button" className={`chip${stateId === s.id ? " on" : ""}`} aria-pressed={stateId === s.id} onClick={() => set("state", s.id === "open" ? "" : s.id)}>
              {s.label}
            </button>
          ))}
        </div>
        <label className="check">
          <input type="checkbox" checked={muted} onChange={(e) => set("muted", e.target.checked ? "" : "0")} /> Include muted
        </label>
        <form className="grp" onSubmit={applyCheck} role="search" aria-label="Filter by check">
          <input type="search" placeholder="check code, e.g. stream-004" aria-label="Check code" value={checkText} onChange={(e) => setCheckText(e.target.value)} />
          <button type="submit" className="btn">
            Filter
          </button>
        </form>
      </div>
      <QueryView q={q} what="findings">
        {(page) => {
          const label = labelOf([...page.items.map((x) => x.entity), ...muteAccounts]);
          return (
            <div className="card others">
              {page.items.length === 0 ? (
                <Empty>{stateId === "open" && sev.length === 0 && !check && !account ? "All clear: no finding fires in this range." : "No findings match these filters."}</Empty>
              ) : (
                page.items.map((f) => <FindingRow key={f.id} f={f} end={end} rangeStart={start} label={label} />)
              )}
              <Pager page={pager.page} next={page.nextPageToken} onNext={() => pager.next(page.nextPageToken)} onPrev={pager.prev} total={page.totalCount} shown={page.items.length} />
              {account && (page.otherAccountCount ?? 0) > 0 && (
                <p className="muted-note">
                  {page.otherAccountCount === 1 ? "1 finding of another account is" : `${page.otherAccountCount} findings of other accounts are`} not shown; findings of servers, clients and the system are, as they concern every account.{" "}
                  <button type="button" className="linkish" onClick={() => setAccount("")}>
                    Show all accounts
                  </button>
                </p>
              )}
            </div>
          );
        }}
      </QueryView>
      <Mutes others={accountsOf(q.data?.items.map((f) => f.entity) ?? [])} onAccounts={setMuteAccounts} />
    </>
  );
}

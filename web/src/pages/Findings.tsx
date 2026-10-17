// Findings: every finding in the range by severity and state, worst first,
// and the mutes with who set them, why and until when.
import { useState, type FormEvent } from "react";
import { useSearchParams } from "react-router";
import type { FindingState, Mute, SeverityLevel } from "../api/model";
import { useFindings, useMutes } from "../api/queries";
import { Empty, QueryView, Section } from "../components/common";
import { Pager, usePager } from "../components/EntityParts";
import { EntityLine, FindingRow } from "../components/FindingParts";
import { UnmuteDialog } from "../components/MuteDialog";
import { Sev } from "../components/Severity";
import { fmtDuration, isoMinute } from "../lib/format";
import { usePageTitle } from "../lib/usePageTitle";
import { useSystem } from "../lib/useSystem";
import { rangeKey } from "../lib/timeRange";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";

const SEVERITIES: SeverityLevel[] = ["critical", "warning", "info"];

const STATES: { id: string; label: string; states: FindingState[] }[] = [
  { id: "open", label: "Firing", states: [] },
  { id: "flapping", label: "Flapping", states: ["flapping"] },
  { id: "resolved", label: "Resolved", states: ["resolved"] },
  { id: "all", label: "All", states: ["firing", "changed", "flapping", "resolved"] },
];

function MuteRow({ m, now, onLift }: { m: Mute; now: number; onLift: (m: Mute) => void }) {
  const active = m.unmutedAt === undefined && (m.expiresAt ?? 0) > now;
  return (
    <div className="frow mute">
      <span>
        <Sev level="muted" label={active ? "Muted" : m.unmutedAt !== undefined ? "Lifted" : "Expired"} />
      </span>
      <span className="code">{m.checkCode}</span>
      <span>
        <span className="e">{m.entity ? <EntityLine e={m.entity} small /> : "every entity"}</span>
      </span>
      <span className="s">
        {m.expiresAt !== undefined && (active ? `ends in ${fmtDuration(m.expiresAt - now)}` : `ended ${isoMinute(m.unmutedAt ?? m.expiresAt)}`)}
        {active && (
          <>
            <br />
            <button type="button" className="btn" onClick={() => onLift(m)}>
              Lift…
            </button>
          </>
        )}
      </span>
      <span className="mute-why">
        <b>Muted by {m.mutedBy || "unknown"}</b>
        {m.createdAt !== undefined && ` on ${isoMinute(m.createdAt)}`}: {m.reason}
        {m.expiresAt !== undefined && ` Expires ${isoMinute(m.expiresAt)} UTC.`}
        {m.unmutedAt !== undefined && ` Lifted ${isoMinute(m.unmutedAt)} by ${m.unmutedBy || "unknown"}.`}
      </span>
    </div>
  );
}

function Mutes() {
  const [inactive, setInactive] = useState(false);
  const [lifting, setLifting] = useState<Mute>();
  const pager = usePager([useSystem(), String(inactive)].join("|"));
  const q = useMutes(inactive, pager.token);
  const now = Date.now();
  return (
    <Section
      title="Mutes"
      aside="A muted finding still records transitions and never alerts. Every mute ends."
      right={
        <label className="check chip">
          <input type="checkbox" checked={inactive} onChange={(e) => setInactive(e.target.checked)} /> Include ended
        </label>
      }
    >
      <QueryView q={q} what="mutes">
        {(page) =>
          page.items.length ? (
            <div className="card others mutes-list">
              {page.items.map((m) => (
                <MuteRow key={m.id} m={m} now={now} onLift={setLifting} />
              ))}
              {(page.nextPageToken || pager.page > 1) && (
                <Pager page={pager.page} next={page.nextPageToken} onNext={() => pager.next(page.nextPageToken)} onPrev={pager.prev} total={page.totalCount} shown={page.items.length} />
              )}
            </div>
          ) : (
            <Empty>No mutes{inactive ? "" : " in effect"}.</Empty>
          )
        }
      </QueryView>
      <UnmuteDialog mute={lifting} onClose={() => setLifting(undefined)} />
    </Section>
  );
}

export function Findings() {
  usePageTitle("Findings");
  const [params, setParams] = useSearchParams();
  const { spec } = useTimeRange();
  const { start, end } = useResolvedRange(spec);
  const sev = (params.get("sev") ?? "").split(",").filter((s): s is SeverityLevel => SEVERITIES.includes(s as SeverityLevel));
  const stateId = STATES.find((s) => s.id === params.get("state"))?.id ?? "open";
  const muted = params.get("muted") !== "0";
  const check = params.get("check") ?? "";
  const [checkText, setCheckText] = useState(check);
  const pager = usePager([useSystem(), sev.join(), stateId, muted, check, rangeKey(spec)].join("|"));
  const q = useFindings(spec, {
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
      <div className="page-h">
        <span className="aside">A finding is one failing check for one entity. Unmuted first, then by severity, then the longest firing.</span>
      </div>
      <div className="filters">
        <div className="grp" role="group" aria-label="Severity">
          <span className="lbl">Severity</span>
          <button type="button" className={`chip${sev.length === 0 ? " on" : ""}`} aria-pressed={sev.length === 0} onClick={() => set("sev", "")}>
            All
          </button>
          {SEVERITIES.map((s) => (
            <button key={s} type="button" className={`chip${sev.includes(s) ? " on" : ""}`} aria-pressed={sev.includes(s)} onClick={() => toggleSev(s)}>
              {s}
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
        {(page) => (
          <div className="card others">
            {page.items.length === 0 ? (
              <Empty>{stateId === "open" && sev.length === 0 && !check ? "All clear: no finding fires in this range." : "No findings match these filters."}</Empty>
            ) : (
              page.items.map((f) => <FindingRow key={f.id} f={f} end={end} rangeStart={start} />)
            )}
            <Pager page={pager.page} next={page.nextPageToken} onNext={() => pager.next(page.nextPageToken)} onPrev={pager.prev} total={page.totalCount} shown={page.items.length} />
          </div>
        )}
      </QueryView>
      <Mutes />
    </>
  );
}

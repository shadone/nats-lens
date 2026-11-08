import { Remediation } from "./Remediation";
import { useState, type ReactNode } from "react";
import { useParams } from "react-router";

import { useLensInfo, useOnCall } from "../api/queries";
import { serverNow } from "../lib/clock";
import { fmtDuration, isoMinute } from "../lib/format";
import { atHistoryStart } from "../lib/historyView";
import { assetStatus, contextText, headline, rightNow, safePlaybook, storyNote, remediationParts } from "../lib/onCallView";
import { replayEntry } from "../lib/replayEntry";
import { ReplayContextLink } from "./ReplayEntry";
import { usePageTitle } from "../lib/usePageTitle";
import { useSystem } from "../lib/useSystem";
import { entityName, findingPath } from "../lib/view";
import { FindingAcknowledgement } from "./Acknowledgement";
import { Empty, QueryView, RLink } from "./common";
import { HistoryLink } from "./HistoryNote";
import { Sev } from "./Severity";
export function OnCallFinding({ allDetails }: { allDetails: ReactNode }) {
  const id = useParams().id ?? "";
  const sys = useSystem();
  const q = useOnCall(id);
  const info = useLensInfo().data;
  const [details, setDetails] = useState(false);
  usePageTitle(q.data?.finding?.checkName ?? id);
  return <div className="oc-page">
    <nav className="crumbs" aria-label="Breadcrumb"><RLink to="/findings">Findings</RLink><span aria-hidden="true">/</span><span>On call</span></nav>
    <QueryView q={q} what="the on-call finding">{d => {
      const f = d.finding;
      if (!f) return <Empty>No finding with this ID.</Empty>;
      const known = atHistoryStart(f, info?.historyStart);
      const since = f.conditionSince ?? f.since;
      const playbook = safePlaybook(d.playbookUrl);
      const remediation = remediationParts(f.remediation);
      const replay = d.storyA !== undefined && d.storyB !== undefined ? replayEntry({a:d.storyA,b:d.storyB},serverNow(),info?.collectIntervalMs || 30_000) : undefined;
      const note = storyNote(d.steps.length, d.stepsTotal, d.stepsNote);
      return <>
        <section className={`oc-card card worst ${f.severity}`}>
          <div className="oc-meta"><Sev level={f.severity} /><span>{sys || info?.cluster || "System"}</span><RLink to={`/checks/${encodeURIComponent(f.checkCode)}`}>{f.checkCode} · Check reference</RLink></div>
          <h1 className="oc-title">{headline(f, d.sameCheckFiring)}</h1>
          <p>{f.detail}</p>
          <p className="oc-time">{f.state === "resolved" ? `Resolved${f.resolvedAt !== undefined ? ` ${isoMinute(f.resolvedAt)} UTC` : ""}. Present observations describe now.` : since !== undefined ? `Since ${isoMinute(since)} UTC (${fmtDuration(Math.max(0, serverNow() - since))})` : "Start not reported"}</p>
          {known && <p><HistoryLink>already true when nats-lens started</HistoryLink></p>}
          {d.contextWindow && <p className="oc-context">{contextText(d.contextWindow)}. Inferred from observed events.</p>}
          <h2 className="oc-heading">Affected assets</h2>
          <ul className="oc-assets">{d.assets.map(a => <li key={a.findingId}>
            <RLink to={findingPath(a.findingId)}>{entityName(a.entity) || "system"}{a.entity?.kind === "consumer" && ` on ${a.entity.stream}`}</RLink><span>{assetStatus(a)}</span>
          </li>)}</ul>
          {d.assetsTotal > d.assets.length && <p><RLink to={`/findings?check=${encodeURIComponent(f.checkCode)}`}>and {d.assetsTotal - d.assets.length} more</RLink></p>}
        </section>
        <section className="oc-card card"><h2 className="oc-heading">Right now</h2><p>{rightNow(d.now)}</p>{d.now.epochAt !== undefined && <p className="oc-time">Last collection {isoMinute(d.now.epochAt)} UTC</p>}</section>
        <section className="oc-card card"><h2 className="oc-heading">What happened</h2>
          {d.steps.length ? <ol className="oc-story">{d.steps.map(s => <li key={s.id}><time>{isoMinute(s.time)} UTC</time><span>{s.title}</span></li>)}</ol> : !note && <p>No steps recorded in this story.</p>}
          {note && <p className="oc-note">{note}{/history|retention/i.test(note) && <> <HistoryLink /></>}</p>}
          {replay && <div className="replay-entry"><ReplayContextLink to={replay.to} /><p>{replay.text} Surrounding observations, not the finding's duration. {replay.notes.join(" ")}</p></div>}
        </section>
        <section className="oc-card card"><h2 className="oc-heading">What to do</h2>
          {remediation.warning ? <>
            <div className="oc-warning"><Remediation text={remediation.warning} /></div>
            {remediation.steps.length > 0 && <ol className="oc-steps">{remediation.steps.map((text, i) => <li key={i}><Remediation text={text} /></li>)}</ol>}
          </> : <p>The check gives no remediation.</p>}
          {playbook && <p><a href={playbook} className="x" rel="noreferrer">Open the playbook</a></p>}
        </section>
        <FindingAcknowledgement key={`${sys}:${id}`} finding={f} phone />
        <details className="oc-details" onToggle={e => setDetails(e.currentTarget.open)}><summary>All details</summary>{details && allDetails}</details>
      </>;
    }}</QueryView>
  </div>;
}

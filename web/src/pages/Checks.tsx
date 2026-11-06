import { useState } from "react";
import { useParams } from "react-router";
import type { CheckDefinition, ListCheckDefinitionsResponse } from "../gen/natslens/v1/checks_pb";
import { useCheckDefinitions } from "../api/checks";
import { Empty, KV, QueryView, Remediation, RLink, Section } from "../components/common";
import { usePageTitle } from "../lib/usePageTitle";

function ConfigurationNote({ state }: { state: string }) {
  return <p className="muted-note">{state === "configured"
    ? "Configured values belong to the selected system now. They do not reconstruct settings at the selected historical range. Reference rules describe shipped defaults."
    : state === "demo"
      ? "Demo reference: these are shipped defaults. The demo does not run checks or supply effective configuration."
      : "Check configuration is unavailable for this system. These are shipped defaults; enabled state and effective values are unknown."}</p>;
}

export function Checks() {
  usePageTitle("Checks");
  const q = useCheckDefinitions();
  const [search, setSearch] = useState("");
  const [entity, setEntity] = useState("");
  return <>
    <div className="page-h"><h1>Checks</h1><span className="aside">What each check means, when it fires, and what to inspect next.</span></div>
    <QueryView q={q} what="the check catalog">{(d) => {
      const term = search.trim().toLowerCase();
      const rows = d.definitions.filter(c => (!entity || c.entity === entity) && `${c.code} ${c.name} ${c.category} ${c.description}`.toLowerCase().includes(term));
      return <>
        <ConfigurationNote state={d.configurationState} />
        <form className="check-filters" role="search" onSubmit={e => e.preventDefault()}>
          <label>Search checks<input type="search" value={search} onChange={e => setSearch(e.target.value)} placeholder="Code, name, or meaning" /></label>
          <label>Applies to<select value={entity} onChange={e => setEntity(e.target.value)}><option value="">All entities</option>{[...new Set(d.definitions.map(c => c.entity))].sort().map(e => <option key={e} value={e}>{e}</option>)}</select></label>
        </form>
        <p role="status">{rows.length} of {d.definitions.length} checks</p>
        {rows.length ? <ul className="check-catalog">{rows.map(c => <li key={c.code} className="card">
          <RLink to={`/checks/${encodeURIComponent(c.code)}`}><span className="code">{c.code}</span><h2>{c.name}</h2></RLink>
          <p className="check-meta">{c.entity} · {c.category} · {c.severity}{c.severityRule ? ", varies by rule" : ""}{c.disabled === true ? " · Disabled" : c.disabled === false ? " · Enabled" : ""}</p>
          <p>{c.description}</p>
        </li>)}</ul> : <Empty>No checks match. Change the search or entity filter.</Empty>}
      </>;
    }}</QueryView>
  </>;
}

function Definition({ c, catalog }: { c: CheckDefinition; catalog: ListCheckDefinitionsResponse }) {
  const configured = catalog.configurationState === "configured";
  return <article className="check-reference">
    <div className="page-h"><h1>{c.name}</h1><span className="aside code">{c.code}</span></div>
    <p className="check-meta">{c.entity} · {c.category} · {c.kind} · Default severity: {c.severity}</p>
    <Section title="What it means"><Remediation text={c.description} /></Section>
    <Section title="When it fires and what the evidence establishes" aside="Reference rules use shipped defaults. Compare named thresholds with the values below.">
      {c.reference.map((section, i) => <div className="check-paragraph" key={i}><h3>{section.title}</h3><Remediation text={section.text} /></div>)}
      {c.severityRule && <div className="check-paragraph"><h3>Severity rule</h3><Remediation text={c.severityRule} /></div>}
      {c.valueName && <div className="check-paragraph"><h3>Finding value</h3><Remediation text={`${c.valueName}${c.valueBasis ? `: ${c.valueBasis}` : ""}`} /></div>}
      {c.suppressedBy.length > 0 && <p>Suppressed for the same entity while {c.suppressedBy.map((code,i) => <span key={code}>{i > 0 ? ", " : ""}<RLink to={`/checks/${code}`}>{code}</RLink></span>)} fires.</p>}
    </Section>
    <Section title="Defaults and configuration">
      <ConfigurationNote state={catalog.configurationState} />
      {configured && <p><strong>{c.disabled ? "Disabled for this system" : "Enabled for this system"}</strong>. A disabled check does not evaluate entities.</p>}
      <KV items={[
        ...(c.lookback ? [{ key: "Lookback default", value: c.lookback }] : []),
        ...(c.effectiveLookback !== undefined ? [{ key: "Configured lookback", value: c.effectiveLookback }] : []),
        { key: "Restart exclusion default", value: `${catalog.defaultRestartWindow} (where the rule applies)` },
        ...(catalog.restartWindow !== undefined ? [{ key: "Configured restart exclusion", value: catalog.restartWindow }] : []),
        { key: "Check timeout default", value: catalog.defaultTimeout },
        ...(catalog.timeout !== undefined ? [{ key: "Configured check timeout", value: catalog.timeout }] : []),
      ].map(item => ({ ...item, highlight: false }))} />
      {c.thresholds.length ? <dl className="check-thresholds">{c.thresholds.map(t => <div key={t.name}>
        <dt><code>{t.name}</code> <span className="lbl">{t.unit}</span></dt>
        <dd><p>{t.description}</p><p>Default: <strong>{t.defaultValue}</strong>{t.effectiveValue !== undefined && <> · Configured: <strong>{t.effectiveValue}</strong></>}</p></dd>
      </div>)}</dl> : <p>No named numeric thresholds. The firing rules above still apply.</p>}
      {c.relatedConfig.length > 0 && <p>Related settings to inspect on an affected entity: {c.relatedConfig.map((name,i) => <span key={name}>{i ? ", " : ""}<code>{name}</code></span>)}. Entity settings are shown with a finding when captured.</p>}
    </Section>
    <Section title="Next steps"><Remediation text={c.remediation} /><p className="muted-note">This is the shipped guidance. A finding may also include private guidance for the selected system. nats-lens observes NATS; it performs no repair.</p></Section>
    <Section title="Investigate in context"><p><RLink to={`/findings?check=${encodeURIComponent(c.code)}`}>Findings for {c.code}</RLink></p><p>The link keeps the selected system and time range. A quiet finding list does not prove health: missing observations, disabled checks, and failed evaluations can leave a condition unreported.</p></Section>
  </article>;
}

export function CheckDetail() {
  const { code = "" } = useParams();
  const q = useCheckDefinitions();
  usePageTitle(q.data?.definitions.find(c => c.code === code)?.name ?? code);
  return <>
    <nav className="crumbs" aria-label="Breadcrumb"><RLink to="/checks">Checks</RLink><span aria-hidden="true">/</span><span className="code">{code}</span></nav>
    <QueryView q={q} what="the check reference">{d => {
      const c = d.definitions.find(c => c.code === code);
      return c ? <Definition c={c} catalog={d} /> : <><h1>Check not found</h1><Empty>No check with code <code>{code}</code>. <RLink to="/checks">Browse all checks</RLink>.</Empty></>;
    }}</QueryView>
  </>;
}

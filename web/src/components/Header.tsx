// Sticky top bar: brand, cluster, servers, time range, theme, and tabs.
import { NavLink } from "react-router";
import { useHeaderStatus, useLensInfo } from "../api/queries";
import { useTimeRange } from "../lib/useTimeRange";
import { RangeNotice, RangePicker } from "./RangePicker";
import { ThemeSwitch } from "./ThemeSwitch";

const TABS: [string, string][] = [
  ["/", "Overview"],
  ["/servers", "Servers"],
  ["/streams", "Streams"],
  ["/consumers", "Consumers"],
  ["/connections", "Connections"],
  ["/events", "Events"],
  ["/findings", "Findings"],
];

export function Header() {
  const { spec, query } = useTimeRange();
  const info = useLensInfo().data;
  const status = useHeaderStatus(info?.collectIntervalMs ?? 30_000);
  const o = status.data;
  const cluster = o?.cluster || info?.cluster;
  return (
    <header className="top">
      <div className="wrap">
        <div className="bar">
          <NavLink to={`/${query}`} className="brand" aria-label="nats-lens overview">
            <i aria-hidden="true" />
            nats-lens
          </NavLink>
          <div className="crumb">
            <span className="lbl">Cluster</span>
            <b>{cluster || "–"}</b>
            {info?.demo && <span className="demo-tag">demo</span>}
          </div>
          {/* The chips show the servers now, whatever range the page shows. */}
          <div className={`srv${status.error ? " stale" : ""}`} aria-label={spec.kind === "fixed" ? "Servers now" : "Servers"}>
            {spec.kind === "fixed" && o && <em className="now-lbl">now</em>}
            {o?.servers.map((s) => (
              <span key={s.name} className={s.present ? undefined : "down"} title={s.present ? `${s.name} answered` : `${s.name} did not answer`}>
                {s.name} {s.version}
              </span>
            ))}
          </div>
          <RangePicker freshness={o?.freshness} error={status.error ?? undefined} />
          <ThemeSwitch />
        </div>
        <RangeNotice />
        <nav className="tabs" aria-label="Pages">
          {TABS.map(([to, label]) => (
            <NavLink key={to} to={`${to}${query}`} end={to === "/"} className={({ isActive }) => (isActive ? "on" : undefined)}>
              {label}
            </NavLink>
          ))}
        </nav>
      </div>
    </header>
  );
}

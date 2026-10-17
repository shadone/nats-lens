// The sidebar: the mark, the system switcher and the pages in groups, each
// link carrying the range, system and account. It collapses to a rail of
// icons; at phone width it is a drawer the header's menu button opens.
import { useEffect, useRef } from "react";
import { NavLink } from "react-router";
import { useShellStatus } from "../api/queries";
import { withGlobal } from "../lib/globalQuery";
import { useGlobalQuery } from "../lib/useGlobalQuery";
import type { Level, Tally } from "../api/model";
import { visibleNav, type BadgeSource, type NavGroup, type NavItem } from "../nav";
import { SystemSwitcher } from "./SystemSwitcher";

export interface Badge {
  count: number;
  level: Level;
  /** Said after the count: "3 firing". */
  what: string;
  /** The worst state in words, when it is not the level's own name. */
  worst?: string;
}

/** Unmuted firing findings, at the level of the worst. */
export function findingsBadge(t: Tally | undefined): Badge | undefined {
  if (!t) return undefined;
  const count = t.critical + t.warning + t.info;
  if (count === 0) return undefined;
  const level: Level = t.critical ? "critical" : t.warning ? "warning" : "info";
  return { count, level, what: "firing" };
}

/**
 * Each badge source's count. Fleet counts systems that are not OK once
 * ListSystems returns each system's status; until then it has no badge.
 */
function useBadges(): Partial<Record<BadgeSource, Badge>> {
  const tally = useShellStatus().data?.tally;
  return { findings: findingsBadge(tally) };
}

function groups(items: NavItem[]): [NavGroup, NavItem[]][] {
  const out: [NavGroup, NavItem[]][] = [];
  for (const i of items) {
    const last = out[out.length - 1];
    if (last && last[0] === i.group) last[1].push(i);
    else out.push([i.group, [i]]);
  }
  return out;
}

function Icon({ d }: { d: string }) {
  return (
    <svg className="ni" viewBox="0 0 18 18" aria-hidden="true">
      <path d={d} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

interface Props {
  collapsed: boolean;
  onCollapse: (collapsed: boolean) => void;
  drawerOpen: boolean;
  onCloseDrawer: () => void;
}

export function Sidebar({ collapsed, onCollapse, drawerOpen, onCloseDrawer }: Props) {
  const global = useGlobalQuery();
  const badges = useBadges();
  const close = useRef<HTMLButtonElement>(null);
  // An opened drawer takes focus, so the keyboard continues inside it.
  useEffect(() => {
    if (drawerOpen) close.current?.focus();
  }, [drawerOpen]);
  return (
    <aside className={`side${collapsed ? " collapsed" : ""}${drawerOpen ? " open" : ""}`} id="sidebar">
      <div className="side-top">
        <NavLink to={withGlobal("/", global)} className="brand" aria-label="nats-lens overview">
          <svg viewBox="0 0 22 22" aria-hidden="true">
            <circle cx="11" cy="11" r="9" fill="none" stroke="currentColor" strokeWidth="2" />
            <circle cx="11" cy="11" r="3.5" fill="currentColor" />
          </svg>
          <span className="side-txt">nats-lens</span>
        </NavLink>
        <button ref={close} type="button" className="side-close" aria-label="Close menu" onClick={onCloseDrawer}>
          <svg viewBox="0 0 14 14" aria-hidden="true">
            <path d="M3 3l8 8M11 3l-8 8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
        </button>
      </div>
      <SystemSwitcher compact={collapsed} />
      <nav aria-label="Pages" className="side-nav">
        {groups(visibleNav()).map(([group, items]) => (
          <div className="side-grp" key={group || "top"}>
            {group && <div className="side-gh lbl">{group}</div>}
            <ul>
              {items.map((i) => {
                const b = i.badge ? badges[i.badge] : undefined;
                return (
                  <li key={i.path}>
                    <NavLink to={withGlobal(i.path, global)} end={i.path === "/"} title={collapsed ? i.label : undefined} aria-label={b ? `${i.label}, ${b.count} ${b.what}, worst ${b.level}` : undefined} className={({ isActive }) => (isActive ? "on" : undefined)}>
                      <Icon d={i.icon} />
                      <span className="side-txt">{i.label}</span>
                      {b && (
                        <span className={`nb ${b.level}`} aria-hidden="true">
                          {b.count}
                        </span>
                      )}
                    </NavLink>
                  </li>
                );
              })}
            </ul>
          </div>
        ))}
      </nav>
      <button type="button" className="side-collapse" aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"} onClick={() => onCollapse(!collapsed)}>
        <svg viewBox="0 0 18 18" aria-hidden="true">
          <path d={collapsed ? "M7 4l5 5-5 5" : "M11 4 6 9l5 5"} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
        <span className="side-txt">Collapse</span>
      </button>
    </aside>
  );
}

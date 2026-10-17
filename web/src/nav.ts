// The pages of the sidebar, in order. A page's own route lives in App.tsx;
// this list says where it sits, what it is called and what its badge counts.
// A planned page stays here, hidden, until its page exists.

export type NavGroup = "" | "Messaging" | "JetStream" | "Cluster";

/** What a badge counts: systems not OK (Fleet), unmuted firing findings (Findings). */
export type BadgeSource = "fleet" | "findings";

export interface NavItem {
  path: string;
  label: string;
  group: NavGroup;
  /** SVG path data on an 18x18 grid, drawn as a 1.6 stroke. */
  icon: string;
  /** Not built yet: hidden from the sidebar. */
  planned?: boolean;
  /** A stand-in page that a later page replaces. */
  placeholder?: boolean;
  badge?: BadgeSource;
  /** Reads no system: the page shows every system. */
  crossSystem?: boolean;
  /** The list page applies the account scope, so the header offers it. */
  accountScope?: boolean;
}

export const NAV: NavItem[] = [
  { path: "/fleet", label: "Fleet", group: "", icon: "M3 3h5v5H3Z M10 3h5v5h-5Z M3 10h5v5H3Z M12.5 10v5 M10 12.5h5", badge: "fleet", crossSystem: true },
  { path: "/", label: "Overview", group: "", icon: "M3 9 9 3l6 6M5 7.5V15h8V7.5" },
  { path: "/findings", label: "Findings", group: "", icon: "M9 3 16 15H2Z M9 8v3 M9 13v.5", badge: "findings" },
  { path: "/subjects", label: "Subjects", group: "Messaging", icon: "M3 5h12 M3 9h8 M3 13h5 M13 11l3 2-3 2", planned: true },
  { path: "/micro-services", label: "Micro services", group: "Messaging", icon: "M3 3h5v5H3Z M10 10h5v5h-5Z M8 5.5h4.5V10", planned: true },
  { path: "/applications", label: "Applications", group: "Messaging", icon: "M5 5a2 2 0 1 0 0 .1 M13 5a2 2 0 1 0 0 .1 M9 13a2 2 0 1 0 0 .1 M6 6.5l2 5 M12 6.5l-2 5", planned: true },
  { path: "/connections", label: "Connections", group: "Messaging", icon: "M2 9h4 M12 9h4 M6 6h6v6H6Z", accountScope: true },
  { path: "/streams", label: "Streams", group: "JetStream", icon: "M2 6c3-3 5 3 8 0s4-1 6 0 M2 12c3-3 5 3 8 0s4-1 6 0" },
  { path: "/consumers", label: "Consumers", group: "JetStream", icon: "M3 9h8 M8 5l4 4-4 4 M14 4v10", accountScope: true },
  { path: "/kv-stores", label: "KV Stores", group: "JetStream", icon: "M4 4h4v4H4Z M10 5h5 M4 10h4v4H4Z M10 11h5", accountScope: true },
  { path: "/object-stores", label: "Object Stores", group: "JetStream", icon: "M3 6 9 3l6 3v7l-6 3-6-3Z M3 6l6 3 6-3 M9 9v7", accountScope: true },
  { path: "/placement", label: "Placement", group: "JetStream", icon: "M3 3h5v5H3Z M10 3h5v5h-5Z M3 10h5v5H3Z M10 10h5v5h-5Z", planned: true },
  { path: "/capacity", label: "Capacity", group: "JetStream", icon: "M3 15V9 M7 15V6 M11 15V10 M15 15V3", planned: true },
  { path: "/servers", label: "Servers", group: "Cluster", icon: "M3 3h12v5H3Z M3 10h12v5H3Z M6 5.5h.5 M6 12.5h.5" },
  { path: "/events", label: "Events", group: "Cluster", icon: "M3 4h12 M3 9h12 M3 14h8" },
  { path: "/topology", label: "Topology", group: "Cluster", icon: "M4 5a2 2 0 1 0 0 .1 M14 5a2 2 0 1 0 0 .1 M9 14a2 2 0 1 0 0 .1 M6 5h6 M5 7l3 5 M13 7l-3 5", planned: true },
  { path: "/accounts", label: "Accounts", group: "Cluster", icon: "M9 8a3 3 0 1 0 0-.1 M3 16c1-3 3.5-4.5 6-4.5s5 1.5 6 4.5", planned: true },
  { path: "/users", label: "Users & auth", group: "Cluster", icon: "M6 8V6a3 3 0 0 1 6 0v2 M4 8h10v7H4Z", planned: true },
];

/** The pages the sidebar shows. */
export function visibleNav(items: NavItem[] = NAV): NavItem[] {
  return items.filter((i) => !i.planned);
}

/** The sidebar page a path belongs to: "/streams/APP/order-jobs" belongs to Streams. */
export function navFor(pathname: string, items: NavItem[] = NAV): NavItem | undefined {
  if (pathname === "/") return items.find((i) => i.path === "/");
  return items.find((i) => i.path !== "/" && (pathname === i.path || pathname.startsWith(`${i.path}/`)));
}

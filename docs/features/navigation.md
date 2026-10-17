# Navigation, time range and theme

- **Surfaces:** `ui`
- **Status:** shipped
- **Related:** [overview-page.md](overview-page.md), [history-and-archive.md](history-and-archive.md), [command-bar.md](command-bar.md), [multiple-systems.md](multiple-systems.md), [mobile.md](mobile.md), [design.md](../design.md)

## What it does

Every page of the web UI shares one header. It names the cluster, shows every server with its version and whether it answered, holds the one time range that every page reads, says how fresh the data is, and switches the theme. Tabs below it lead to the seven pages: Overview, Servers, Streams, Consumers, Connections, Events and Findings. The time range lives in the page address, so a link shows the next person exactly what you saw.

## Behavior and rules

- **The header.** It shows the nats-lens mark (a link to the Overview), the cluster name ("demo" beside it in demo mode), and one chip per server with its name and version. A server that did not answer in the newest collection is shown as down. The chips always show the servers now, also when the page shows a past range; then they are labelled "now".
- **Tabs.** Overview, Servers, Streams, Consumers, Connections, Events and Findings. A tab keeps the current time range.
- **One global time range.** Every page reads the same range. Live presets are "Last 1h", "Last 6h", "Last 24h" (the default) and "Last 7d"; a live range moves with the clock. A custom range has a start and an end in UTC, at most 30 days long, and not in the future. The header shows the range's span in UTC.
- **Live and fixed.** The freshness button pauses a live range at its current span, and turns a fixed range back into the closest live preset.
- **Shareable links.** The range is in the address as `?range=1h|6h|24h|7d` for live, or `?from=…&to=…` (UTC, such as `2026-01-05T09:00:00Z`) for fixed. A link with a range that cannot be used shows the last 24 hours instead, says why in a notice, and offers "Remove it from the link".
- **Freshness.** The header says "live" with the age of the newest collection; "stale" when that collection is older than three collect intervals; "no data" when the range holds no collection; "fixed" for a fixed range; and "offline" when the status call to nats-lens fails. Its tooltip names the newest collection and whether it was partial.
- **What a list shows.** Lists of servers, streams, consumers and connections show the newest collection inside the range. Charts and growth cover the whole range.
- **Theme.** The theme button cycles through system, light and dark. The choice is kept in the browser; "system" follows the operating system.
- **Times are UTC.** Every time in the UI is UTC.
- **Phone width.** Tables scroll sideways inside their own box, so the page itself never scrolls sideways.

## Scenarios

### Share what I am looking at

- **Given** I chose a custom range from 09:00 to 10:00 UTC
- **When** I copy the page's address and a colleague opens it
- **Then** the colleague sees the same page for the same hour, with "fixed" in the header

### A link carries a broken range

- **Given** a link whose range ends before it starts
- **When** I open it
- **Then** the page shows the last 24 hours and a notice that the link's range does not start before it ends
- **And** "Remove it from the link" clears the range from the address

### See that collection stopped

- **Given** NATS has been unreachable for five minutes with a 30-second interval
- **When** I look at the header
- **Then** it says "stale" with the age of the newest collection

### Switch to the dark theme

- **Given** the theme follows the system
- **When** I press the theme button twice
- **Then** the UI is dark, and stays dark the next time I open nats-lens in this browser

## Planned

- **A sidebar.** A collapsible sidebar replaces the tabs, with the pages in groups: Fleet, Overview, Findings and Replay at the top; Messaging (Subjects, Micro services, Applications); JetStream (Placement, Streams, Consumers, KV Stores, Object Stores, Capacity); and Cluster (Servers, Topology, Accounts, Users and auth, Events). Items carry a badge with the number of findings that need attention, and a switcher at the top moves between systems ([multiple-systems.md](multiple-systems.md)). Collapsed, it shows only the icons.
- **An account filter.** A selector in the top bar limits the pages to one account or shows all of them.
- **A theme menu.** The theme button opens a menu with Light, Dark and System, each with its icon and a check mark on the current choice, instead of cycling.
- **A command bar.** A search box with a keyboard shortcut ([command-bar.md](command-bar.md)).

## Not supported / out of scope

- **No time zone setting.** Times are always UTC.
- **No custom range over 30 days** in the UI, although the API accepts ranges up to 400 days.
- **No saved views.** The address is the only way to keep a page with its range and filters.
- **No sign-in.** The UI has no accounts of its own; an identity-aware proxy in front of it decides who may open it.

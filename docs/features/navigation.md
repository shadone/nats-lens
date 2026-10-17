# Navigation, time range and theme

- **Surfaces:** `ui`
- **Status:** shipped
- **Related:** [overview-page.md](overview-page.md), [history-and-archive.md](history-and-archive.md), [command-bar.md](command-bar.md), [multiple-systems.md](multiple-systems.md), [mobile.md](mobile.md), [design.md](../design.md)

## What it does

Every page of the web UI sits in one shell. A sidebar on the left lists the pages in groups and shows which system the pages read. A header above the page names it, limits it to one account or shows all, searches the system's servers, streams, consumers and buckets, holds the one time range that every page reads, says how fresh the data is, and switches the theme. The range, the system and the account live in the page address, so a link shows the next person exactly what you saw.

## Behavior and rules

- **The sidebar.** It shows the nats-lens mark (a link to the Overview), the system switcher, and the pages in groups: Fleet, Overview and Findings at the top; Messaging with Connections; JetStream with Streams and Consumers; Cluster with Servers and Events. A detail page marks the page it belongs to, so a stream's page marks Streams. Pages that are not built yet are not listed.
- **Badges.** Findings carries the number of unmuted firing findings. The badge has the shape of the worst of them: filled for critical, outlined for warning, dashed for info. Without a firing finding there is no badge. Fleet has no badge yet.
- **Collapse.** "Collapse" at the foot of the sidebar shrinks it to a rail of icons, with the badges on the icons; the choice is kept in the browser. The rail hides the system switcher.
- **The system switcher.** It names the system, with its environment, the number of servers ("3 servers", or "2 of 3 servers" while some do not answer) and "demo" in demo mode. With several systems it opens a menu of them; picking one opens the same list page on that system (from a stream's page, Streams), keeping the range and leaving the account scope. "All systems" opens Fleet. With one system it is no menu.
- **Fleet.** With more than one system, opening nats-lens without a system in the address lands on Fleet, which lists the systems by name, each a link to its Overview. With one system it lands on the Overview.
- **The header.** It names the page, then holds the account scope, the search, the time range with the freshness, and the theme button. On Fleet, which shows every system, it has no account scope.
- **Account scope.** "Account" opens a menu: "All accounts" (the button says "All" and the number of accounts) or one account of the system, the system account marked "system". The choice goes into the address as `?account=` and follows every link. Consumers lists the consumers of that account, unless a stream is chosen; Connections lists its connections. Other pages show every account for now.
- **Search.** Typing part of a name lists up to eight servers, streams, consumers, KV stores, object stores and accounts of the current system whose name contains it, names that start with it first. The arrow keys move, Enter or a click opens the page (a KV store or object store opens its stream; an account sets the account scope), and Escape clears and closes. Ctrl+K, or Cmd+K on a Mac, puts the cursor in the box. The names load the first time the box gets focus.
- **The system in the address.** `?system=` names the system a page reads. Without it a page reads the default system, the first one configured; with several systems its links then name the default system. Every link to another page keeps the system, the account scope and the range. A link that names another system leaves the account scope behind, since accounts belong to one system.
- **An unknown system.** A link whose `?system=` names no configured system shows "System not found" with a link to Fleet, asks nothing of that system, and the sidebar's links lead to the default system.
- **One global time range.** Every page reads the same range. Live presets are 1h, 6h, 24h (the default) and 7d; the range button shows the chosen one as, for example, "Last 24h". A live range moves with the clock. A custom range has a start and an end in UTC, at most 30 days long, starting no later than now. The header shows the range's span in UTC.
- **Live and fixed.** The freshness button pauses a live range at its current span, and turns a fixed range back into the closest live preset.
- **Shareable links.** The range is in the address as `?range=1h|6h|7d` for live (the default 24 hours leaves the parameter out; `?range=24h` is also read), or `?from=…&to=…` (UTC, such as `2026-01-05T09:00:00Z`) for fixed. A link with a range that cannot be used shows the last 24 hours instead, says why in a notice, and offers "Remove it from the link".
- **Freshness.** The header asks for the last two collect intervals only, whatever range the page shows. It says "live" with the age of the newest collection; "no data" when no collection started in the last two collect intervals, as when NATS is unreachable; "stale" only while a shown collection is older than three intervals and the next refresh has not landed; "fixed" for a fixed range; and "offline" when the status call to nats-lens fails. Its tooltip names the newest collection and whether it was partial.
- **What a list shows.** Lists of servers, streams, consumers and connections show the newest collection inside the range. Charts and growth cover the whole range.
- **Theme.** One button shows the current theme's icon and a chevron. It opens a menu with Light, Dark and System, each with its icon, and a check on the current one. The choice is kept in the browser; System follows the operating system.
- **Keyboard.** Every menu opens with a click, Enter or the down arrow; the arrow keys move, Enter picks, and Escape closes it and returns to its button.
- **Times are UTC.** Every time in the UI is UTC.
- **Phone width.** The sidebar becomes a drawer that the menu button in the header opens; it closes when a page opens, on Escape, or on a tap beside it. The range and the search take a line each. Tables scroll sideways inside their own box, so the page itself never scrolls sideways.

## Scenarios

### Share what I am looking at

- **Given** I chose a custom range from 09:00 to 10:00 UTC on system `example-global`, scoped to account `APP`
- **When** I copy the page's address and a colleague opens it
- **Then** the colleague sees the same page of the same system and account for the same hour, with "fixed" in the header

### Move to another system

- **Given** I look at the stream `order-jobs` of `example-prod` over the last 6 hours, scoped to account `APP`
- **When** I pick `example-global` in the system switcher
- **Then** the Streams page of `example-global` opens over the last 6 hours, for all accounts

### Open a link to a system that is gone

- **Given** a link with `?system=old-staging`, a system this nats-lens does not observe
- **When** I open it
- **Then** the page says "System not found" and links to Fleet

### Jump to a consumer

- **Given** any page of a system
- **When** I press Ctrl+K, type `resize`, and press Enter on `resize-worker`
- **Then** the consumer's page opens on the same system and range

### See how many findings fire

- **Given** two warnings and one info fire, and none is muted
- **When** I look at the sidebar
- **Then** Findings has an outlined badge with 3

### A link carries a broken range

- **Given** a link whose range ends before it starts
- **When** I open it
- **Then** the page shows the last 24 hours and a notice that the link's range does not start before it ends
- **And** "Remove it from the link" clears the range from the address

### See that collection stopped

- **Given** NATS has been unreachable for five minutes with a 30-second interval
- **When** I look at the header
- **Then** it says "no data"

### Switch to the dark theme

- **Given** the theme follows the system
- **When** I open the theme menu and pick Dark
- **Then** the UI is dark, the menu shows the check on Dark, and the UI stays dark the next time I open nats-lens in this browser

### Use it on a phone

- **Given** a screen 390 pixels wide
- **When** I tap the menu button and then Servers
- **Then** the sidebar slides in, and closes as the Servers page opens

## Planned

- **More pages in the sidebar.** Subjects, Micro services and Applications under Messaging; KV Stores, Object Stores, Placement and Capacity under JetStream; Topology, Accounts and Users and auth under Cluster. Each appears once its page is built.
- **A Fleet page with a card per system** and a Fleet badge with the number of systems that are not OK ([multiple-systems.md](multiple-systems.md)).
- **The account scope on every list.** Streams, findings and events limited to the scoped account; system-wide tiles and charts stay system-wide and say "all accounts".
- **A command bar.** Findings, events and prefixes such as `stream:` in the search ([command-bar.md](command-bar.md)).

## Not supported / out of scope

- **No time zone setting.** Times are always UTC.
- **No custom range over 30 days** in the UI, although the API accepts ranges up to 400 days.
- **No saved views.** The address is the only way to keep a page with its range and filters.
- **No sign-in.** The UI has no accounts of its own; an identity-aware proxy in front of it decides who may open it.
- **No search across systems.** The search reads the current system only.

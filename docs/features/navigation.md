# Navigation, time range and theme

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [data-history.md](data-history.md), [overview-page.md](overview-page.md), [history-and-archive.md](history-and-archive.md), [command-bar.md](command-bar.md), [multiple-systems.md](multiple-systems.md), [mobile.md](mobile.md), [design.md](../design.md)

## What it does

Every page of the web UI sits in one shell. A sidebar on the left lists the pages in groups and shows which system the pages read. A header above the page names it, limits a list to one account where the list supports it, searches the system's servers, streams, consumers and buckets, holds the one time range that every page reads, says how fresh the data is, and switches the theme. The range, the system and the account live in the page address, so a link shows the next person exactly what you saw.

## Behavior and rules

- **Bucket links.** KV and object-store links open dedicated bucket routes across resource surfaces. Legacy storage-stream links redirect to the bucket page with their query context; an explicitly labeled secondary action retains access to low-level stream diagnostics. See [KV and object stores](kv-and-object-stores.md).

- **Page position and focus.** Opening a new path starts at the top and focuses its heading when available. Range, account, system, and filter query changes keep the reading position and focus. Browser Back and Forward restore the position saved for that history entry, including after delayed content arrives. A saved Back or Forward position takes priority over the entry’s fragment. Initial fragment links scroll to and focus their target. Following the same fragment link again also scrolls to and focuses its target. Query-only changes with an unchanged fragment keep the reading position. Pending restoration stops when the reader takes control, or after ten seconds. Position history lasts while the UI stays open and keeps up to 100 entries.

- **Replay entry links.** Overview observed windows and finding details open [Replay](replay.md) with explicit A/B and a fixed range, retaining the system and using the shared account policy (Replay is system-wide). Links support keyboard and native modified-click navigation.

- **Phone finding links.** The [phone finding card](mobile.md) uses the same finding URL as desktop. Alert-label links resolve query parameters into that URL.

- **Short history.** Range pages show a note below the header when the range begins more than one collect interval before history starts. It names the available span and links to Data history, preserving the system, range, and originating account query. Data history itself is system-wide. Fleet and Data history do not repeat this note.

- **The sidebar.** It shows the nats-lens mark (a link to the Overview), the system switcher, and the pages in groups: Fleet, Overview and Findings at the top; Messaging with Subjects, Micro services, Applications and Connections; JetStream with Streams, Consumers, KV Stores, Object Stores, Placement and Capacity; Cluster with Servers, Events, Replay, Topology, Accounts and Data history. A detail page marks the page it belongs to, so a stream's page marks Streams. Pages that are not built yet are not listed.
- **Badges.** Findings carries the number of unmuted firing findings. The badge has the shape of the worst of them: filled for critical, outlined for warning, dashed for info, and a screen reader hears "Findings, 3 firing, worst critical". Without a firing finding there is no badge. Fleet carries the number of systems that are not OK (critical, warning, not collecting, summary unavailable or no data), shaped by the worst; a system whose only findings are info is OK and not counted, and a screen reader hears "Fleet, 2 not OK, worst critical"; it counts over the last 24 hours whatever range the page shows. With every system OK there is no badge. The Fleet badge can trail the Fleet cards by up to two collect intervals: nats-lens keeps each system's badge state for one interval and serves it once more while it reads it again. A system whose first state takes longer than half a second to read is left out of the count until a later refresh has it.
- **Collapse.** "Collapse" at the foot of the sidebar shrinks it to a rail of icons; the choice is kept in the browser. In the rail a badge becomes a small mark on its icon's top right corner, with the badge's shape (a dot for critical, a ring for warning, a dashed ring for info, a dotted ring for no data) and no number; the count is in the link's tooltip, such as "Findings, 6 firing", and a screen reader hears the same name as in the full sidebar. At phone width the drawer shows the full sidebar with its numbers, whatever the collapse choice. The rail shows the system as its initials, with the name as a tooltip, and they open the same system menu.
- **The system switcher.** It names the system, with its environment, the number of servers ("3 servers", or "2 of 3 servers" while some do not answer) and "Demo" in demo mode. With several systems it opens a menu of them; picking one opens the same list page on that system (from a stream's page, Streams), keeping the range and leaving the account scope. "All systems" opens Fleet. With one system it is no menu.
- **Fleet.** With more than one system, opening nats-lens without a system in the address lands on Fleet, a card per system that opens its Overview ([multiple-systems.md](multiple-systems.md)). With one system it lands on the Overview.
- **The header.** It names the page, then holds the account scope on the pages that apply it, the search, the time range with the freshness, and the theme button. On a page of the sidebar the name is the page's one heading, and the page below starts with a muted line that says what it lists, such as "Client connections in the newest collection of the range."; the page does not repeat its name. A detail page, such as a stream's, has its own heading with the entity's name, and the header then names the section without being a heading. On the Overview the status line is status text, not a heading, and the cause cards sit under a "Findings" heading that only screen readers hear.
- **Account scope.** The Subjects, Micro services, Applications, Connections, Consumers, KV Stores and Object Stores lists offer it; other pages do not show it. Unscoped the button reads "All accounts", scoped "Account: APP". It opens a menu: "All accounts" with their number, such as "All accounts (3)", and each account of the system, the system account marked "system". The choice goes into the address as `?account=` and follows the links to those lists and their detail pages; a link to any other page leaves it behind. Consumers lists the consumers of that account, unless a stream is chosen; Connections lists its connections; Subjects lists its patterns and tests a subject published in it; Micro services lists its services; Applications lists the applications of its connections. A change of the scope shows none of the previous account's rows while the new ones load.
- **Focus follows the page.** Opening another page moves the keyboard focus to the page's content, so Tab and a screen reader start there; the first load and a change of only the range, filters or system leave the focus where it is. The sidebar is a landmark named "Navigation".
- **Search.** Typing part of a name lists up to eight servers, streams, consumers, KV stores, object stores and accounts of the current system whose name contains it, names that start with it first. The arrow keys move, and Enter or a click opens the page: a KV store or object store opens its stream, an account the Connections list scoped to it. The first Escape closes the list and the second clears the text; the cursor stays in the box. Cmd+K on a Mac, or Ctrl+K elsewhere, puts the cursor in the box, unless the cursor is in another text field. The names load the first time the box gets focus, and never for a system that is not configured.
- **The system in the address.** `?system=` names the system a page reads. Without it a page reads the default system, the first one configured; with several systems its links then name the default system. Every link to another page keeps the system and the range, and the account scope where that page applies it. A link that names another system leaves the account scope behind, since accounts belong to one system.
- **Another system starts afresh.** A page opened on another system shows none of the previous system's rows while its own load, and its filters and page start over.
- **An unknown system.** A link whose `?system=` names no configured system shows "System not found" and "No system x. See Fleet.", asks nothing of that system but the shell's status call, which says the system is not configured without reading anything of it, and the sidebar's links lead to the default system.
- **A mute stays on its system.** Mute and Lift mute act on the system the dialog was opened on, even when the address changes while it is open.
- **One global time range.** Every page reads the same range. Live presets are 1h, 6h, 24h (the default) and 7d; the range button shows the chosen one as, for example, "Last 24h". A live range moves with the server's clock: before the shell shows, a page load reads the server's now from `GetLensInfo` (its `now`), and every "now" of the UI, a live range's start among them, follows that clock, so a browser whose clock differs from the server's still gets a range that starts before the server's now. When that call fails or takes longer than 3 seconds the UI uses the browser's clock (the 3 seconds proposed, not yet confirmed). A custom range has a start and an end in UTC, at most 30 days long, starting no later than now. For a live preset the header shows only its label. Opening the range popover shows both exact bounds in the browser's date format, labelled UTC. The custom text fields use the same browser date order, digits and 12- or 24-hour clock as the bounds, with a four-digit Gregorian year and a format example. Entries must follow that format; invalid calendar dates and times leave the editor open with an error. Values and shared links remain UTC. A fixed range also keeps its span in the header.
- **Live and fixed.** The freshness button pauses a live range at its current span, and turns a fixed range back into the closest live preset.
- **Shareable links.** The range is in the address as `?range=1h|6h|7d` for live (the default 24 hours leaves the parameter out; `?range=24h` is also read), or `?from=…&to=…` (UTC, such as `2026-01-05T09:00:00Z`) for fixed. A link with a range that cannot be used shows the last 24 hours instead, says why in a notice, and offers "Remove it from the link".
- **One status call for the shell.** The header, the sidebar's badges, the system switcher, the system list and the "System not found" check read one call, `GetShellStatus`, for the page's system: once when the page loads and once per collect interval, the interval its answer names. It carries the system's freshness, servers and findings tally, and every system with its badge state. When the system's own part fails or takes longer than 10 seconds, the call still lists the systems and their badges, and the header says "offline" with the reason. Switching system keeps the list of systems and the Fleet badge while the new system's status loads; the header waits for the new system's own. On top of it a page load asks `GetLensInfo` once for the server's now and once for the page's system, and each page its own calls; the Fleet page keeps its own `ListSystems` over its range.
- **Freshness.** The header asks for the last two collect intervals only, whatever range the page shows. It says "live" without a ticking age counter; "not collecting since 11:30" while the collector's connection to NATS is not connected or closed (the date too when that began a day or more ago), or "not collecting" when no collector writes to the database; "no data" only when no collection exists and the collector collects; "stale" only while the system collects, a shown collection is older than three intervals and the next refresh has not landed; "fixed" for a fixed range; and "offline" when the status call to nats-lens fails. Its tooltip gives the collection age when available and names the newest collection and whether it was partial ("fixed" and "no data" have none). While the collector does not collect, the header looks up the newest collection before the stop once per stop, so the system switcher keeps its number of servers. It looks back at most two days: after a stop older than that, the switcher shows no server count and the tooltip names no collection.
- **What a list shows.** Lists of servers, streams, consumers and connections show the newest collection inside the range. Charts and growth cover the whole range.
- **Theme.** One button shows the current theme's icon and a chevron. It opens a menu with Light, Dark and System, each with its icon, and a check on the current one. The choice is kept in the browser; System follows the operating system.
- **Keyboard.** Every menu opens with a click, Enter or the down arrow; the arrow keys move, Enter picks, and Escape closes it and returns to its button. In the system menu the arrow keys also reach "All systems", which opens Fleet.
- **A narrow window.** The header never makes the page scroll sideways, from 360 px wide up. When the time range does not fit on its line, a fixed range's dates give way first and the freshness last, each cut with an ellipsis; the dates' tooltip holds both times, the freshness's tooltip its whole text, and a screen reader hears the freshness as the description of the live button. This order is proposed, not yet confirmed.
- **Times are UTC.** Every time in the UI is UTC.
- **Phone width.** The sidebar becomes a drawer that the menu button in the header opens. While it is open, Tab stays inside it. It closes when a link opens, on its close button, on Escape (outside an open menu), or on a tap beside it. Navigation gives focus to the destination heading or explicit fragment after the drawer closes. Closing without navigation returns focus to the menu button. The range and the search take a line each. Tables scroll sideways inside their own box, so the page itself never scrolls sideways.

- **Finding links from alerts.** `/findings/by-labels` reads URL-encoded `system`, `code`, and `entity_key`, then replaces the route with `/findings/<id>?system=<id>`. A missing finding opens Findings filtered to its check, with an explanation that it may have resolved and been swept. Both destinations retain the global time range, system, and account, dropping label-routing and unknown parameters. Other API failures remain errors. The same canonical finding URL shows the [phone card](mobile.md) at 760 px and below.
## Scenarios

### Share what I am looking at

- **Given** I chose a custom range from 09:00 to 10:00 UTC on the Connections list of system `example-global`, scoped to account `APP`
- **When** I copy the page's address and a colleague opens it
- **Then** the colleague sees the same page of the same system and account for the same hour, with "fixed" in the header

### Move to another system

- **Given** I look at the Consumers list of `example-prod` over the last 6 hours, scoped to account `APP`
- **When** I pick `example-global` in the system switcher
- **Then** the Consumers list of `example-global` opens over the last 6 hours, for all accounts, without a row of `example-prod`

### Open a link to a system that is gone

- **Given** a link with `?system=old-staging`, a system this nats-lens does not observe
- **When** I open it
- **Then** the page says "System not found" and links to Fleet

### Jump to a consumer

- **Given** any page of a system
- **When** I press Cmd+K (Ctrl+K off a Mac), type `resize`, and press Enter on `resize-worker`
- **Then** the consumer's page opens on the same system and range

### See how many findings fire

- **Given** two warnings and one info fire, and none is muted
- **When** I look at the sidebar
- **Then** Findings has an outlined badge with 3

### A system with only info findings leaves Fleet unbadged

- **Given** two systems, one OK and one whose only firing finding is info
- **When** I look at the sidebar
- **Then** Fleet has no badge, and Findings still counts the info finding

### A link carries a broken range

- **Given** a link whose range ends before it starts
- **When** I open it
- **Then** the page shows the last 24 hours and a notice that the link's range does not start before it ends
- **And** "Remove it from the link" clears the range from the address

### See that collection stopped

- **Given** NATS has been unreachable since 11:30 with a 30-second interval
- **When** I look at the header at 11:35
- **Then** it says "not collecting since 11:30"
- **And** the system switcher still says "3 servers", from the newest collection before 11:30

### Read the freshness in a narrow window

- **Given** system `example-staging` has not collected since 15:22
- **When** I open its Subjects page in a window 761 px wide, beside the open sidebar
- **Then** the header reads "not collecting since 15:22" in full and the page does not scroll sideways
- **And** opening the range popover shows both exact bounds, labelled UTC

### Load a page with one status call for the shell

- **Surfaces:** `ui`, `api`
- **Given** the Streams page of a system with a 30-second interval
- **When** I open it and leave it open for a minute
- **Then** the shell asks `GetShellStatus` once on load and once every 30 seconds, and never `ListSystems` or `GetOverview`

### Switch to the dark theme

- **Given** the theme follows the system
- **When** I open the theme menu and pick Dark
- **Then** the UI is dark, the menu shows the check on Dark, and the UI stays dark the next time I open nats-lens in this browser

### Use it on a phone

- **Given** a screen 390 pixels wide
- **When** I tap the menu button and then Servers
- **Then** the sidebar slides in, and closes as the Servers page opens

### Open a detail after reading a long list

- **Surfaces:** `ui`
- **Given** I have scrolled down the Checks catalog or another list
- **When** I open a detail page
- **Then** the detail starts at the top with its heading focused
- **And** the link retains the system, time range, and applicable account context
- **When** I use browser Back
- **Then** the list returns to its saved reading position, waiting for delayed content if needed

### Keep reading when the range changes

- **Surfaces:** `ui`
- **Given** I am reading partway down a detail page
- **When** I change only the range or another query parameter
- **Then** the UI keeps my reading position and focus
- **When** I follow an explicit fragment link
- **Then** the UI scrolls to and focuses the fragment target

### Follow the same fragment again

- **Surfaces:** `ui`
- **Given** I followed a fragment link and then scrolled farther down the page
- **When** I follow the same fragment link again
- **Then** the UI scrolls to and focuses the fragment target again

### Follow a fragment from the phone drawer

- **Surfaces:** `ui`
- **Given** the phone drawer is open
- **When** I follow a fragment link, including one to the same page
- **Then** the drawer closes and the fragment target receives focus when it arrives
- **And** the page heading does not replace that focus

### Return to a fragment entry

- **Surfaces:** `ui`
- **Given** I opened a fragment link and then scrolled to another position
- **When** I open another page and use browser Back
- **Then** the UI restores my saved reading position, including after delayed content arrives
- **And** the original fragment does not move me away from that position

## Planned

- **More pages in the sidebar.** Users and auth under Cluster, once its page is built.
- **The account scope on more lists.** Streams, findings and events limited to the scoped account, each offering the scope once it applies it; system-wide tiles and charts stay system-wide and say "all accounts".
- **A command bar.** Findings, events and prefixes such as `stream:` in the search ([command-bar.md](command-bar.md)).

### Follow a short-history note

- **Surfaces:** `ui`
- **Given** the chosen system has 31 minutes of history and the page shows the last 24 hours
- **When** I follow the note’s Data history link
- **Then** Data history opens for the same system and range

### Open an alert on a phone

- **Surfaces:** `ui`
- **Given** an alert link contains the URL-encoded system, code, and entity key of an existing finding
- **When** an operator opens it in a viewport at most 760 px wide
- **Then** the canonical finding URL shows the phone card
- **And** encoded literal plus signs, spaces, slashes, and colons retain their meaning

### Inspect a live range without header clutter

- **Surfaces:** `ui`
- **Given** the selected range is Last 24h
- **When** I read the header
- **Then** the button shows Last 24h without exact dates beside it, and the live label has no seconds counter
- **When** I open the range popover
- **Then** both bounds appear in my browser's date format, labelled UTC
- **And** the editable From and To values use the same date order, digits and hour cycle as those bounds
- **And** typing a valid custom range in the displayed format keeps UTC timestamps in its shared link
- **And** an invalid day, hour or missing AM/PM leaves the editor open with an error

## Not supported / out of scope

- **No calendar popup.** Custom dates and times are entered as text in the displayed browser format, using the Gregorian calendar.
- **No time zone setting.** Times are always UTC.
- **No custom range over 30 days** in the UI, although the API accepts ranges up to 400 days.
- **No saved views.** The address is the only way to keep a page with its range and filters.
- **No sign-in.** The UI has no accounts of its own; an identity-aware proxy in front of it decides who may open it.
- **No search across systems.** The search reads the current system only.

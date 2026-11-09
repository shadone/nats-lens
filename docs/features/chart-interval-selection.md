# Chart interval selection

- **Surfaces:** `ui`
- **Status:** shipped
- **Related:** [overview-page.md](overview-page.md), [capacity.md](capacity.md), [navigation.md](navigation.md), [chart-help.md](chart-help.md)

## What it does

Select time on a storage chart, then choose whether to change the page range or investigate storage contributors. Selecting alone does not navigate or change the range.

## Behavior and rules

- Supported server, resource and aggregate metric titles and selected intervals lead to [metric investigations](metric-investigations.md), preserving system, range and reported server scope. Storage and detections reuse specialist pages.


- Drag horizontally in either direction to highlight time. A shared dialog shows editable UTC endpoints and the captured system/account scope. The selection uses the chart frame at gesture start; refreshing the chart does not move the endpoints or close the dialog.
- **Set time range** freezes the global range at the chosen endpoints. Shared URLs preserve milliseconds. Back restores the previous range. Existing endpoint pins are cleared when the range changes.
- **Investigate change** opens the storage comparison's contributors view with explicit system, account and fixed endpoints. Endpoint availability and retained evidence rules still apply; selecting time does not establish a cause or guarantee matching collections.
- The initial automatic scope is storage-byte charts: Overview storage, Capacity total/server/resource histories, and shared stream storage charts. Unrelated metrics do not imply storage attribution. Callers can explicitly enable generic time selection without the storage action.
- Focus the plot and use Shift with arrow keys, then Enter. The floating Select interval control opens a UTC time picker without dragging and remains available on touch screens. Ordinary point inspection, copy actions and detail links remain available.
- Proposed, not yet confirmed: a four-pixel horizontal threshold distinguishes a drag from a click. Tiny clicks retain existing navigation. Vertical touch gestures retain scrolling; cancelled gestures and Escape do not change the range.
- Valid endpoints must be different, ordered, inside the captured frame and within the shared maximum range. The dialog uses the shared focus and scroll lock. Navigation or scope changes dismiss stale selections.

## Scenarios

### Choose an action after selecting storage time

- **Given** an Overview or Capacity storage chart
- **When** I drag across part of its time axis
- **Then** the interval is highlighted and its UTC endpoints appear, while the current page range stays unchanged.

### Share a fixed range and return

- **Given** an interval selected on a live chart
- **When** I choose Set time range and then use Back
- **Then** the fixed URL preserves the endpoints and Back restores the previous live range.

### Investigate the selected storage interval

- **Given** a selected storage interval and account scope
- **When** I choose Investigate change
- **Then** the contributor comparison opens with that system, account and fixed interval, subject to its existing evidence limits.

### Select without a pointer drag

- **Given** a supported chart
- **When** I use Shift and arrow keys followed by Enter, or open Select interval
- **Then** I can edit UTC endpoints and choose the same actions, or cancel without changing the page.

## Not supported / out of scope

- Selection does not interpolate missing measurements, identify causes or create universal collection pins.
- Arbitrary metric drill-down, categorical plots, Timeline marks and cross-chart brushing are separate capabilities.
- Selecting one resource does not filter the system-wide storage comparison to that resource; the comparison exposes its own contributors and filters.

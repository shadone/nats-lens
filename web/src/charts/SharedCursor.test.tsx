// The Overview's shared moment: hovering, touching or focusing one chart,
// or hovering the lanes, draws a bare line in every other chart over the
// page's range and down the lanes; leaving, blur and Escape clear it.
import { Profiler, type ReactNode } from "react";
import { act, fireEvent, screen, within } from "@testing-library/react";
import type { UseQueryResult } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { Timeline } from "../api/model";
import { StoryLanes } from "../components/StoryLanes";
import { TrendChart } from "../components/TrendChart";
import { useCapturedEvent, useSelectedTimelineEvents } from "../api/timelineEvidence";
import { renderApp } from "../test/render";
import { ChartIntervalProvider } from "./ChartIntervals";
import { MiniLine } from "./MiniLine";
import { SharedCursorProvider } from "./sharedCursor";
import { Sparkline } from "./Sparkline";

vi.mock("../lib/useWidth", () => ({ useWidth: () => 800 }));
vi.mock("../lib/useSystem", async (original) => ({ ...(await original<typeof import("../lib/useSystem")>()), useSystem: () => "example-prod" }));
vi.mock("../api/timelineEvidence", () => ({ useCapturedEvent: vi.fn(), useSelectedTimelineEvents: vi.fn() }));
vi.mock("../api/systems", () => ({ useSystems: () => ({ data: { defaultSystem: "example-prod", systems: [{ id: "example-prod" }] } }), useCurrentSystem: () => ({ id: "example-prod", known: true }) }));

const result = <T,>(data: T) => ({ data, isPending: false, error: null }) as UseQueryResult<T, Error>;
const start = Date.parse("2026-10-17T09:00:00Z"), end = start + 60 * 60_000, step = 5 * 60_000;
const at = (m: number) => start + m * 60_000;
// CPU has no bucket at 09:10: a follower draws the line there all the same, with no value.
const cpu = [0, 5, 15, 20].map((m) => ({ time: at(m), value: m + 1 }));
const tl: Timeline = { start, bucketMs: step, bucketCount: 12, rows: [], kubernetes: [], nats: [], windows: [], natsTruncated: false, findingTruncated: false, kubernetesTruncated: false, eventReadId: "1" };
const box = { left: 0, width: 600, top: 0, height: 40, right: 600, bottom: 40, x: 0, y: 0, toJSON: () => ({}) } as DOMRect;

function Page({ other, children }: { other?: ReactNode; children?: ReactNode }) {
  return (
    <ChartIntervalProvider>
      <SharedCursorProvider start={start} end={end}>
        <Sparkline points={cpu} start={start} end={end} stepMs={step} status="ok" windows={[]} label="CPU" fmt={(v) => `${v}%`} />
        <TrendChart intervalActions title="Messages" start={start} end={end} stepMs={step} layers={[{ kind: "line", label: "in", style: { tone: "accent" }, points: [{ time: at(10), value: 7 }] }]} />
        <MiniLine label="orders delivery" points={[]} level="ok" period="1 h" fmt={String} frame={{ start, end, stepMs: step }} />
        {/* A seven-day chart does not share the hour's range. */}
        <MiniLine label="week" points={[]} level="ok" period="7 d" fmt={String} frame={{ start: end - 7 * 24 * 3600_000, end, stepMs: 3600_000 }} />
        {other}
        <StoryLanes tl={tl} end={end} live={false} />
        {children}
      </SharedCursorProvider>
    </ChartIntervalProvider>
  );
}

const plot = (name: string) => screen.getByRole("slider", { name: `Inspect ${name}` });
const line = (name: string) => plot(name).querySelector(".chart-shared-cursor") as HTMLElement;
const left = (el: HTMLElement) => parseFloat(el.style.left);
const lanesAt = () => parseFloat(lanes().style.getPropertyValue("--cursor-x"));
const shown = (name: string) => line(name).hasAttribute("data-on");
const lanes = () => document.querySelector(".sl-grid") as HTMLElement;
const tracks = () => [...document.querySelectorAll<HTMLElement>(".sl-track")];

beforeEach(() => {
  vi.mocked(useCapturedEvent).mockReturnValue(result(undefined));
  vi.mocked(useSelectedTimelineEvents).mockReturnValue(result({ items: [], totalCount: 0, nextPageToken: "" }));
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(box);
});
afterEach(() => vi.restoreAllMocks());

it("moves a bare line in the other charts and down the lanes while one chart is hovered", () => {
  renderApp(<Page />);
  // 09:10 is a sixth of the hour: 100 of 600 pixels.
  fireEvent.pointerMove(plot("CPU"), { clientX: 100 });
  expect(screen.getAllByRole("tooltip")).toHaveLength(1);
  expect(screen.getByRole("tooltip")).toHaveTextContent("value: No sample");
  expect(shown("CPU")).toBe(false);
  for (const name of ["Messages", "orders delivery"]) {
    expect(shown(name)).toBe(true);
    expect(left(line(name))).toBeCloseTo(16.667, 2);
  }
  expect(shown("week")).toBe(false);
  expect(lanes()).toHaveAttribute("data-cursor");
  expect(lanesAt()).toBeCloseTo(16.667, 2);
  // The line takes neither focus nor a name.
  expect(line("Messages")).toHaveAttribute("aria-hidden", "true");
  expect(line("Messages")).not.toHaveAttribute("tabindex");
  // Moving on to another chart moves the moment and its readout with it.
  fireEvent.pointerMove(plot("Messages"), { clientX: 300 });
  expect(screen.getAllByRole("tooltip")).toHaveLength(1);
  expect(screen.getByRole("tooltip")).toHaveTextContent("in: No sample");
  expect(shown("Messages")).toBe(false);
  expect(left(line("CPU"))).toBeCloseTo(50.000, 2);
});

it("never invents a value in a follower: the line carries none, and the chart's own readout says No sample", () => {
  renderApp(<Page />);
  fireEvent.pointerMove(plot("Messages"), { clientX: 100 });
  expect(screen.getByRole("tooltip")).toHaveTextContent("in: 7");
  // CPU follows with a line only: no readout, no text, no value.
  expect(shown("CPU")).toBe(true);
  expect(line("CPU")).toBeEmptyDOMElement();
  expect(within(plot("CPU")).queryByText(/%/)).toBeNull();
});

it("follows keyboard focus and arrow keys, and clears on blur", () => {
  renderApp(<Page />);
  fireEvent.focus(plot("Messages"));
  expect(shown("CPU")).toBe(true);
  expect(left(line("CPU"))).toBeCloseTo(0.000, 2);
  fireEvent.keyDown(plot("Messages"), { key: "ArrowRight" });
  fireEvent.keyDown(plot("Messages"), { key: "ArrowRight" });
  expect(left(line("CPU"))).toBeCloseTo(16.667, 2);
  expect(lanesAt()).toBeCloseTo(16.667, 2);
  // The focused chart's own readout stays the accessible output.
  expect(plot("Messages")).toHaveAttribute("aria-describedby");
  expect(plot("CPU")).not.toHaveAttribute("aria-describedby");
  fireEvent.blur(plot("Messages"));
  expect(shown("CPU")).toBe(false);
  expect(lanes()).not.toHaveAttribute("data-cursor");
});

it("clears everywhere on Escape, from the focused chart or anywhere on the page", () => {
  renderApp(<Page />);
  fireEvent.focus(plot("CPU"));
  fireEvent.keyDown(plot("CPU"), { key: "End" });
  expect(shown("Messages")).toBe(true);
  fireEvent.keyDown(plot("CPU"), { key: "Escape" });
  expect(shown("Messages")).toBe(false);
  expect(lanes()).not.toHaveAttribute("data-cursor");
  // Hovered, not focused: Escape on the document clears the readout too.
  fireEvent.pointerMove(plot("Messages"), { clientX: 300 });
  expect(shown("CPU")).toBe(true);
  fireEvent.keyDown(document, { key: "Escape" });
  expect(shown("CPU")).toBe(false);
  expect(screen.queryByRole("tooltip")).toBeNull();
});

it("clears after the pointer leaves the chart", () => {
  vi.useFakeTimers();
  try {
    renderApp(<Page />);
    fireEvent.pointerMove(plot("CPU"), { clientX: 300 });
    expect(shown("Messages")).toBe(true);
    fireEvent.pointerLeave(plot("CPU"));
    act(() => void vi.advanceTimersByTime(200));
    expect(shown("Messages")).toBe(false);
    expect(lanes()).not.toHaveAttribute("data-cursor");
  } finally {
    vi.useRealTimers();
  }
});

it("draws the lanes' hover in every chart and down the lanes, and clears when the pointer leaves", () => {
  renderApp(<Page />);
  fireEvent.pointerMove(tracks()[0], { clientX: 450, pointerType: "mouse" });
  for (const name of ["CPU", "Messages", "orders delivery"]) expect(left(line(name))).toBeCloseTo(75.000, 2);
  expect(lanesAt()).toBeCloseTo(75.000, 2);
  expect(screen.queryByRole("tooltip")).toBeNull();
  fireEvent.pointerLeave(tracks()[0]);
  expect(shown("CPU")).toBe(false);
  expect(lanes()).not.toHaveAttribute("data-cursor");
  // A touch on the lanes is a tap on a mark, not a hover.
  fireEvent.pointerMove(tracks()[0], { clientX: 450, pointerType: "touch" });
  expect(shown("CPU")).toBe(false);
});

it("keeps drag selection on a chart and on the lanes, and the lanes' marks still open on a click", () => {
  renderApp(<Page />);
  const p = plot("Messages");
  fireEvent.pointerMove(p, { clientX: 60 });
  fireEvent.pointerDown(p, { pointerId: 1, isPrimary: true, button: 0, clientX: 60, clientY: 20, pointerType: "mouse" });
  fireEvent.pointerMove(p, { pointerId: 1, clientX: 300, clientY: 20, pointerType: "mouse" });
  // While dragging, no moment is shared.
  expect(shown("CPU")).toBe(false);
  fireEvent.pointerUp(p, { pointerId: 1, button: 0, clientX: 300, clientY: 20, pointerType: "mouse" });
  let dialog = screen.getByRole("dialog", { name: "Selected chart interval" });
  expect(Date.parse((within(dialog).getByLabelText("Start (UTC)") as HTMLInputElement).value + "Z")).toBe(at(6));
  expect(Date.parse((within(dialog).getByLabelText("End (UTC)") as HTMLInputElement).value + "Z")).toBe(at(30));
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));

  // The lanes: a drag selects, then Set time range zooms the page in.
  const track = tracks()[0];
  fireEvent.pointerDown(track, { pointerId: 2, isPrimary: true, button: 0, clientX: 300, clientY: 10, pointerType: "mouse" });
  fireEvent.pointerMove(track, { pointerId: 2, clientX: 450, clientY: 10, pointerType: "mouse" });
  expect(track.querySelector(".chart-selected-interval")).not.toBeNull();
  fireEvent.pointerUp(track, { pointerId: 2, button: 0, clientX: 450, clientY: 10, pointerType: "mouse" });
  dialog = screen.getByRole("dialog", { name: "Selected chart interval" });
  expect(dialog).toHaveTextContent("Timeline");
  fireEvent.click(within(dialog).getByRole("button", { name: "Set time range" }));
  const u = new URL(screen.getByTestId("location").textContent!, "http://x");
  expect(Date.parse(u.searchParams.get("from")!)).toBe(at(30));
  expect(Date.parse(u.searchParams.get("to")!)).toBe(at(45));
});

it("opens a lane mark on a plain click and not after a drag that starts on it", () => {
  const withGap: Timeline = { ...tl, servers: [], leaders: [], clients: [], systemEventsCaptured: true, jetstreamEventsCaptured: true, advisoryCaptureGaps: [{ start: at(20), end: at(25), connection: "sys" }] };
  renderApp(
    <ChartIntervalProvider>
      <SharedCursorProvider start={start} end={end}>
        <StoryLanes tl={withGap} end={end} live={false} />
      </SharedCursorProvider>
    </ChartIntervalProvider>,
  );
  const mark = screen.getByRole("button", { name: /Event capture stopped 09:20–09:25 \(system account\)/ });
  const track = mark.closest(".sl-track") as HTMLElement;
  fireEvent.pointerDown(mark, { pointerId: 3, isPrimary: true, button: 0, clientX: 210, clientY: 10, pointerType: "mouse" });
  fireEvent.pointerMove(mark, { pointerId: 3, clientX: 400, clientY: 10, pointerType: "mouse" });
  fireEvent.pointerUp(track, { pointerId: 3, button: 0, clientX: 400, clientY: 10, pointerType: "mouse" });
  fireEvent.click(mark);
  expect(screen.getByRole("dialog", { name: "Selected chart interval" })).toBeInTheDocument();
  expect(screen.queryByRole("dialog", { name: /Event capture stopped/ })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  // A plain click still tells the mark's story.
  fireEvent.pointerDown(mark, { pointerId: 4, isPrimary: true, button: 0, clientX: 210, clientY: 10, pointerType: "mouse" });
  fireEvent.pointerUp(mark, { pointerId: 4, button: 0, clientX: 210, clientY: 10, pointerType: "mouse" });
  fireEvent.click(mark);
  expect(screen.getByRole("dialog", { name: /Event capture stopped/ })).toBeInTheDocument();
});

it("re-renders no follower while another chart is hovered", () => {
  const commits: string[] = [];
  const count = (id: string) => () => commits.push(id);
  renderApp(
    <Page other={<Profiler id="follower" onRender={count("follower")}><TrendChart title="Bytes" start={start} end={end} stepMs={step} layers={[]} /></Profiler>} />,
  );
  commits.length = 0;
  for (let x = 1; x <= 50; x++) fireEvent.pointerMove(plot("CPU"), { clientX: x * 10 });
  expect(left(line("Bytes"))).toBeCloseTo(83.333, 2);
  expect(commits).toEqual([]);
  // The profiler does count the follower once it is the chart under the pointer.
  fireEvent.pointerMove(plot("Bytes"), { clientX: 30 });
  expect(commits.length).toBeGreaterThan(0);
});

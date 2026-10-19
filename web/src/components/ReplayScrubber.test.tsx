import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { clampMoments, type Moments } from "../lib/replayUrl";
import type { Timeline } from "../api/model";
import { chipLefts, handleText, moveHandle, ReplayScrubber } from "./ReplayScrubber";

const T = (s: string) => Date.parse(s);
const NOW = T("2026-10-17T12:00:00Z");
const START = NOW - 6 * 3_600_000;
const BUCKET = 5 * 60_000;
const A = T("2026-10-17T09:15:00Z");
const B = T("2026-10-17T09:50:00Z");

function strip(): Timeline {
  const n = 72;
  return {
    start: START,
    bucketMs: BUCKET,
    bucketCount: n,
    rows: [{ key: "server-004", checkCode: "server-004", checkName: "Server restarted", severity: "warning", cells: Array.from({ length: n }, (_, i) => (i < 2 ? "nodata" : i === 40 ? "warning" : "ok")), now: "ok" }],
    kubernetes: [],
    nats: [],
    windows: [{ start: T("2026-10-17T09:14:00Z"), end: T("2026-10-17T09:50:00Z"), label: "GKE node upgrade", detail: "3 drains" }],
    natsTruncated: false,
    findingTruncated: false,
    kubernetesTruncated: false,
  };
}

/**
 * The scrubber under a parent that keeps the moments and reads what it
 * hears as the page does: clamped to what GetReplay accepts and cut to the
 * second.
 */
function Page({ initial, onChange, strip: st = strip() }: { initial: Moments; onChange: (m: Moments) => void; strip?: Timeline }) {
  const [m, setM] = useState(initial);
  return (
    <ReplayScrubber
      start={START}
      end={NOW}
      live
      now={NOW}
      strip={st}
      moments={m}
      minGapMs={30_000}
      onChange={(next) => {
        onChange(next);
        const c = clampMoments(next, NOW, 30_000);
        setM({ a: Math.floor(c.a / 1000) * 1000, b: Math.floor(c.b / 1000) * 1000 });
      }}
    />
  );
}

function setup(over: { a?: number; b?: number; strip?: Timeline } = {}) {
  const onChange = vi.fn();
  render(<Page initial={{ a: over.a ?? A, b: over.b ?? B }} onChange={onChange} strip={over.strip} />);
  return onChange;
}

afterEach(() => vi.restoreAllMocks());

describe("ReplayScrubber", () => {
  it("offers two sliders that say their moment in words", () => {
    setup();
    const a = screen.getByRole("slider", { name: "Moment A" });
    expect(a).toHaveAttribute("aria-valuetext", "Moment A, 09:15 UTC 17 Oct");
    expect(screen.getByRole("slider", { name: "Moment B" })).toHaveAttribute("aria-valuetext", handleText("b", B));
    expect(a).toHaveAttribute("tabindex", "0");
  });

  it("moves a handle by one bucket with the arrows and ten with Shift", async () => {
    const onChange = setup();
    const a = screen.getByRole("slider", { name: "Moment A" });
    a.focus();
    await userEvent.keyboard("{ArrowRight}");
    expect(onChange).toHaveBeenLastCalledWith({ a: A + BUCKET, b: B });
    await userEvent.keyboard("{ArrowLeft}");
    expect(onChange).toHaveBeenLastCalledWith({ a: A, b: B });
    await userEvent.keyboard("{Shift>}{ArrowLeft}{/Shift}");
    expect(onChange).toHaveBeenLastCalledWith({ a: A - 10 * BUCKET, b: B });
    await userEvent.keyboard("{ArrowDown}");
    expect(onChange).toHaveBeenLastCalledWith({ a: A - 11 * BUCKET, b: B });
  });

  it("sends Home and End to the ends each handle may reach", () => {
    const press = (name: string, key: string) => {
      const onChange = setup();
      const h = screen.getByRole("slider", { name });
      fireEvent.keyDown(h, { key });
      fireEvent.keyUp(h, { key });
      cleanup();
      return onChange.mock.calls.at(-1)?.[0];
    };
    expect(press("Moment A", "Home")).toEqual({ a: START, b: B });
    expect(press("Moment A", "End")).toEqual({ a: B - 30_000, b: B });
    expect(press("Moment B", "Home")).toEqual({ a: A, b: A + 30_000 });
    expect(press("Moment B", "End")).toEqual({ a: A, b: NOW });
  });

  it("keeps A before B, B before now and the span within 24 hours", () => {
    expect(moveHandle({ a: A, b: B }, "a", B + 60_000, 30_000, NOW)).toEqual({ a: B - 30_000, b: B });
    expect(moveHandle({ a: A, b: B }, "b", NOW + 60_000, 30_000, NOW)).toEqual({ a: A, b: NOW });
    expect(moveHandle({ a: A, b: B }, "a", B - 30 * 3_600_000, 30_000, NOW)).toEqual({ a: B - 24 * 3_600_000, b: B });
  });

  it("sets A and B from a Kubernetes window, five minutes wider on each side", async () => {
    const onChange = setup();
    await userEvent.click(screen.getByRole("button", { name: "Replay GKE node upgrade 09:14–09:50 UTC" }));
    expect(onChange).toHaveBeenLastCalledWith({ a: T("2026-10-17T09:09:00Z"), b: T("2026-10-17T09:55:00Z") });
  });

  it("describes the strip, its no-data cells dashed", () => {
    const { container } = render(
      <ReplayScrubber start={START} end={NOW} live now={NOW} strip={strip()} moments={{ a: A, b: B }} minGapMs={30_000} onChange={() => {}} />,
    );
    expect(screen.getAllByRole("img", { name: /^Status per 5 min: no data 06:00–06:10, warning 09:20–09:25$/ })).toHaveLength(1);
    expect(container.querySelector('rect[data-level="nodata"]')).toHaveStyle({ strokeDasharray: "2 1.5" });
  });

  it("says when a moment lies outside the range", () => {
    setup({ a: START - 3_600_000 });
    expect(screen.getByRole("note")).toHaveTextContent("A is before this range. Replay still reads it.");
  });

  it("applies a typed moment only on leaving the field or Enter, not while it is typed", () => {
    const onChange = setup();
    const input = screen.getByLabelText("B (UTC)");
    fireEvent.change(input, { target: { value: "2026-10-01T10:05" } });
    fireEvent.change(input, { target: { value: "2026-10-17T10:05" } });
    expect(onChange).not.toHaveBeenCalled();
    expect(input).toHaveValue("2026-10-17T10:05");
    fireEvent.blur(input);
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenLastCalledWith({ a: A, b: T("2026-10-17T10:05:00Z") });
    fireEvent.change(screen.getByLabelText("A (UTC)"), { target: { value: "2026-10-17T09:20" } });
    fireEvent.keyDown(screen.getByLabelText("A (UTC)"), { key: "Enter" });
    expect(onChange).toHaveBeenLastCalledWith({ a: T("2026-10-17T09:20:00Z"), b: T("2026-10-17T10:05:00Z") });
  });

  it("tells the page of a held arrow key once, when it is let go", () => {
    const onChange = setup();
    const a = screen.getByRole("slider", { name: "Moment A" });
    for (let i = 0; i < 5; i++) fireEvent.keyDown(a, { key: "ArrowRight" });
    expect(onChange).not.toHaveBeenCalled();
    expect(a).toHaveAttribute("aria-valuetext", "Moment A, 09:40 UTC 17 Oct");
    fireEvent.keyUp(a, { key: "ArrowRight" });
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenLastCalledWith({ a: A + 5 * BUCKET, b: B });
  });

  it("tells the page of a drag once, on pointer up", () => {
    const rect = vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({ left: 0, width: 720, top: 0, height: 88, right: 720, bottom: 88, x: 0, y: 0, toJSON: () => ({}) } as DOMRect);
    const onChange = setup();
    const track = document.querySelector(".rs-track")!;
    // 720 px for 6 hours: two pixels a minute, from 06:00.
    fireEvent.pointerDown(track, { button: 0, clientX: 600, pointerId: 1 });
    for (const x of [620, 640, 660]) fireEvent.pointerMove(track, { clientX: x, pointerId: 1 });
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByRole("slider", { name: "Moment B" })).toHaveAttribute("aria-valuetext", "Moment B, 11:30 UTC 17 Oct");
    fireEvent.pointerUp(track, { pointerId: 1 });
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenLastCalledWith({ a: A, b: T("2026-10-17T11:30:00Z") });
    rect.mockRestore();
  });

  it("counts info as ok on the strip, so trouble stands out", () => {
    const s = strip();
    s.rows[0].cells = s.rows[0].cells.map((c) => (c === "ok" ? "info" : c));
    const { container } = render(<ReplayScrubber start={START} end={NOW} live now={NOW} strip={s} moments={{ a: A, b: B }} minGapMs={30_000} onChange={() => {}} />);
    expect(container.querySelectorAll('rect[data-level="info"]')).toHaveLength(0);
    expect(screen.getByRole("img", { name: /^Status per 5 min: no data 06:00–06:10, warning 09:20–09:25$/ })).toBeInTheDocument();
  });

  it("names the window in a bar under the strip and puts the time on the handles", () => {
    setup();
    expect(screen.getByRole("button", { name: /^Replay GKE node upgrade/ })).toHaveTextContent("GKE node upgrade");
    expect(screen.getByRole("slider", { name: "Moment A" })).toHaveTextContent("A 09:15");
    expect(screen.getByRole("slider", { name: "Moment B" })).toHaveTextContent("B 09:50");
  });

  it("labels the axis at whole hours", () => {
    const rect = vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({ left: 0, width: 1000, top: 0, height: 88, right: 1000, bottom: 88, x: 0, y: 0, toJSON: () => ({}) } as DOMRect);
    const { container } = render(<ReplayScrubber start={START} end={NOW} live now={NOW} strip={strip()} moments={{ a: A, b: B }} minGapMs={30_000} onChange={() => {}} />);
    expect([...container.querySelectorAll(".rs-axis span")].map((s) => s.textContent)).toEqual(["07:00", "08:00", "09:00", "10:00", "11:00", "now"]);
    rect.mockRestore();
  });

  it("never lets the chips overlap, B at the right end included", () => {
    const rect = vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({ left: 0, width: 1000, top: 0, height: 88, right: 1000, bottom: 88, x: 0, y: 0, toJSON: () => ({}) } as DOMRect);
    render(<ReplayScrubber start={START} end={NOW} live now={NOW} strip={strip()} moments={{ a: NOW - 3_600_000, b: NOW }} minGapMs={30_000} onChange={() => {}} />);
    const left = (name: string) => parseFloat((screen.getByRole("slider", { name }) as HTMLElement).style.left);
    expect(left("Moment B")).toBe(1000 - 68);
    expect(left("Moment A")).toBeLessThanOrEqual(1000 - 2 * 68);
    rect.mockRestore();
    // The mirror at the left end, and a narrow track.
    expect(chipLefts(0, 5, 1000, 68)).toEqual({ a: 0, b: 68 });
    expect(chipLefts(320, 330, 330, 26)).toEqual({ a: 278, b: 304 });
    expect(chipLefts(400, 600, 1000, 68)).toEqual({ a: 332, b: 600 });
  });

  it("shows a bucket with an info and a muted check as muted", () => {
    const s = strip();
    s.rows = [
      { ...s.rows[0], key: "x", cells: s.rows[0].cells.map(() => "info") },
      { ...s.rows[0], key: "y", cells: s.rows[0].cells.map((_, i) => (i === 30 ? "muted" : "ok")) },
    ];
    const { container } = render(<ReplayScrubber start={START} end={NOW} live now={NOW} strip={s} moments={{ a: A, b: B }} minGapMs={30_000} onChange={() => {}} />);
    expect(container.querySelectorAll('rect[data-level="muted"]')).toHaveLength(1);
  });

  it("applies a held arrow once, whatever other key is let go meanwhile", () => {
    const onChange = setup();
    const a = screen.getByRole("slider", { name: "Moment A" });
    fireEvent.keyDown(a, { key: "ArrowRight" });
    fireEvent.keyDown(a, { key: "Shift" });
    fireEvent.keyUp(a, { key: "Shift" });
    fireEvent.keyDown(a, { key: "ArrowRight" });
    fireEvent.keyUp(a, { key: "ArrowRight" });
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  it("keeps a typed A when a press on B starts a move", () => {
    const onChange = setup();
    const input = screen.getByLabelText("A (UTC)");
    input.focus();
    fireEvent.change(input, { target: { value: "2026-10-17T08:00" } });
    const b = screen.getByRole("slider", { name: "Moment B" });
    fireEvent.pointerDown(b, { button: 0, pointerId: 1 });
    fireEvent.keyDown(b, { key: "ArrowRight" });
    fireEvent.keyUp(b, { key: "ArrowRight" });
    expect(onChange.mock.calls.at(-1)![0]).toEqual({ a: T("2026-10-17T08:00:00Z"), b: B + BUCKET });
  });

  it("holds a typed moment to the handles' rules", () => {
    const onChange = setup();
    const input = screen.getByLabelText("A (UTC)");
    fireEvent.change(input, { target: { value: "2026-12-19T08:00" } });
    fireEvent.blur(input);
    expect(onChange).toHaveBeenLastCalledWith({ a: B - 30_000, b: B });
    const bIn = screen.getByLabelText("B (UTC)");
    fireEvent.change(bIn, { target: { value: "2026-10-18T08:00" } });
    fireEvent.blur(bIn);
    expect(onChange).toHaveBeenLastCalledWith({ a: B - 30_000, b: NOW });
  });

  it("puts the handle back when the pointer is cancelled", () => {
    const rect = vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({ left: 0, width: 720, top: 0, height: 88, right: 720, bottom: 88, x: 0, y: 0, toJSON: () => ({}) } as DOMRect);
    const onChange = setup();
    const track = document.querySelector(".rs-track")!;
    fireEvent.pointerDown(track, { button: 0, clientX: 600, pointerId: 1 });
    fireEvent.pointerCancel(track, { pointerId: 1 });
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByRole("slider", { name: "Moment B" })).toHaveAttribute("aria-valuetext", handleText("b", B));
    rect.mockRestore();
  });

  it("moves from the moments the page shows after a window it clamped", () => {
    const st = strip();
    st.windows = [{ start: NOW - 30 * 3_600_000, end: NOW - 15 * 60_000, label: "GKE node upgrade", detail: "" }];
    const b0 = NOW - 10 * 60_000;
    const onChange = setup({ a: b0 - 24 * 3_600_000, b: b0, strip: st });
    fireEvent.click(screen.getByRole("button", { name: /^Replay GKE node upgrade/ }));
    const b = screen.getByRole("slider", { name: "Moment B" });
    fireEvent.keyDown(b, { key: "ArrowLeft" });
    fireEvent.keyUp(b, { key: "ArrowLeft" });
    expect(onChange).toHaveBeenLastCalledWith({ a: b0 - 24 * 3_600_000, b: b0 - BUCKET });
  });
});

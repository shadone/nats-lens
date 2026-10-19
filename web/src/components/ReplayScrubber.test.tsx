import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Timeline } from "../api/model";
import { handleText, moveHandle, ReplayScrubber } from "./ReplayScrubber";

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

function setup(over: { a?: number; b?: number } = {}) {
  const onChange = vi.fn();
  render(
    <ReplayScrubber start={START} end={NOW} live now={NOW} strip={strip()} moments={{ a: over.a ?? A, b: over.b ?? B }} minGapMs={30_000} onChange={onChange} />,
  );
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
    expect(onChange).toHaveBeenLastCalledWith({ a: A - BUCKET, b: B });
    await userEvent.keyboard("{Shift>}{ArrowLeft}{/Shift}");
    expect(onChange).toHaveBeenLastCalledWith({ a: A - 10 * BUCKET, b: B });
    await userEvent.keyboard("{ArrowDown}");
    expect(onChange).toHaveBeenLastCalledWith({ a: A - BUCKET, b: B });
  });

  it("sends Home and End to the ends each handle may reach", () => {
    const onChange = setup();
    const a = screen.getByRole("slider", { name: "Moment A" });
    const b = screen.getByRole("slider", { name: "Moment B" });
    fireEvent.keyDown(a, { key: "Home" });
    fireEvent.keyUp(a, { key: "Home" });
    expect(onChange).toHaveBeenLastCalledWith({ a: START, b: B });
    fireEvent.keyDown(a, { key: "End" });
    fireEvent.keyUp(a, { key: "End" });
    expect(onChange).toHaveBeenLastCalledWith({ a: B - 30_000, b: B });
    fireEvent.keyDown(b, { key: "Home" });
    fireEvent.keyUp(b, { key: "Home" });
    expect(onChange).toHaveBeenLastCalledWith({ a: A, b: A + 30_000 });
    fireEvent.keyDown(b, { key: "End" });
    fireEvent.keyUp(b, { key: "End" });
    expect(onChange).toHaveBeenLastCalledWith({ a: A, b: NOW });
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
    expect(onChange).toHaveBeenLastCalledWith({ a: T("2026-10-17T09:20:00Z"), b: B });
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
});

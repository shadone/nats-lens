import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
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
    expect(onChange).toHaveBeenLastCalledWith({ a: START, b: B });
    fireEvent.keyDown(a, { key: "End" });
    expect(onChange).toHaveBeenLastCalledWith({ a: B - 30_000, b: B });
    fireEvent.keyDown(b, { key: "Home" });
    expect(onChange).toHaveBeenLastCalledWith({ a: A, b: A + 30_000 });
    fireEvent.keyDown(b, { key: "End" });
    expect(onChange).toHaveBeenLastCalledWith({ a: A, b: NOW });
  });

  it("keeps A before B, B before now and the span within 24 hours", () => {
    expect(moveHandle({ a: A, b: B }, "a", B + 60_000, 30_000, NOW)).toEqual({ a: B - 30_000, b: B });
    expect(moveHandle({ a: A, b: B }, "b", NOW + 60_000, 30_000, NOW)).toEqual({ a: A, b: NOW });
    expect(moveHandle({ a: A, b: B }, "a", B - 30 * 3_600_000, 30_000, NOW)).toEqual({ a: B - 24 * 3_600_000, b: B });
  });

  it("sets A and B from a Kubernetes window, five minutes wider on each side", async () => {
    const onChange = setup();
    await userEvent.click(screen.getByRole("button", { name: "Replay GKE node upgrade 09:14–09:50" }));
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

  it("takes a moment typed in UTC", () => {
    const onChange = setup();
    fireEvent.change(screen.getByLabelText("B (UTC)"), { target: { value: "2026-10-17T10:05" } });
    expect(onChange).toHaveBeenLastCalledWith({ a: A, b: T("2026-10-17T10:05:00Z") });
  });
});

import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Tile } from "../api/model";
import { TileCard } from "../components/Tiles";
import { fmtBytes, fmtCount } from "../lib/format";
import { MiniLine } from "./MiniLine";
import { LimitBar, RangeBar } from "./RangeBar";
import { SeriesChart } from "./SeriesChart";
import { Sparkline } from "./Sparkline";

const start = Date.parse("2026-10-16T05:08:00Z");
const end = Date.parse("2026-10-17T05:08:00Z");
const period = "16 Oct 05:08 to 17 Oct 05:08 UTC";
const pts = [
  { time: start, value: 900 },
  { time: end, value: 1204 },
];

describe("chart accessible names", () => {
  it("Sparkline names the metric, its latest value and the range", () => {
    render(<Sparkline points={pts} status="ok" windows={[]} start={start} end={end} label="Messages pending" current="1,204" />);
    expect(screen.getByRole("img", { name: `Messages pending, current 1,204, ${period}` })).toBeInTheDocument();
  });

  it("Sparkline says there is no data, with the range", () => {
    render(<Sparkline points={[]} status="ok" windows={[]} start={start} end={end} label="Messages pending" />);
    expect(screen.getByRole("img", { name: `Messages pending, no data, ${period}` })).toBeInTheDocument();
  });

  it("SeriesChart gives the latest value with its unit", () => {
    const series = { metric: "stream.bytes", unit: "bytes" as const, stepMs: 900_000, points: [{ time: start, value: 1024 }, { time: end, value: 2048 }], min: 1024, max: 2048 };
    render(<SeriesChart title="Bytes" series={series} start={start} end={end} />);
    expect(screen.getByRole("img", { name: `Bytes, current 2 KiB, ${period}` })).toBeInTheDocument();
  });

  it("MiniLine names the latest value and the period", () => {
    render(<MiniLine points={[{ time: start, value: 1024 }, { time: end, value: 3 * 1024 }]} level="ok" label="jobs bytes" period="last 7 days" fmt={fmtBytes} />);
    expect(screen.getByRole("img", { name: "jobs bytes, current 3 KiB, last 7 days" })).toBeInTheDocument();
  });

  it("MiniLine with one point still names its value", () => {
    render(<MiniLine points={[{ time: end, value: 5 }]} level="ok" label="jobs bytes" period="last 7 days" fmt={fmtCount} />);
    expect(screen.getByRole("img", { name: "jobs bytes, current 5, last 7 days" })).toBeInTheDocument();
  });

  it("RangeBar names the metric, current value, min, max and range", () => {
    render(<RangeBar min={10} max={1204} current={1204} fmt={fmtCount} label="Messages pending" period={period} />);
    expect(screen.getByRole("img", { name: `Messages pending, current 1,204, min 10, max 1,204, ${period}` })).toBeInTheDocument();
  });

  it("LimitBar names the metric and current value", () => {
    render(<LimitBar max={512} current={256} limit={1024} limitText="1 KiB" label="Storage" currentText="256 B" />);
    expect(screen.getByRole("img", { name: "Storage, current 256 B, 25.0% of 1 KiB used" })).toBeInTheDocument();
  });

  it("a tile's sparkline carries the tile's value and unit", () => {
    const t: Tile = {
      id: "storage",
      label: "Stored",
      value: 5 * 1024 ** 3,
      unit: "bytes",
      valueSuffix: "",
      value2Suffix: "",
      subtitle: "",
      status: "ok",
      series: { metric: "m", unit: "bytes", stepMs: 1, points: pts, min: 900, max: 5 * 1024 ** 3 },
    };
    render(<TileCard t={t} windows={[]} start={start} end={end} />);
    expect(screen.getByRole("img", { name: `Stored, current 5 GiB, ${period}` })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: /^Stored, current 5 GiB, min .*, max 5 GiB, 16 Oct/ })).toBeInTheDocument();
  });
});

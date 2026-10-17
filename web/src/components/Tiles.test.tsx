import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Tile } from "../api/model";
import { fmtValue } from "../lib/units";
import { TileCard } from "./Tiles";

const tile = (over: Partial<Tile>): Tile => ({
  id: "t", label: "Bytes in", unit: "bytes_per_second", valueSuffix: "", value2Suffix: "", subtitle: "", status: "ok",
  series: { metric: "m", unit: "bytes_per_second", stepMs: 30_000, points: [{ time: 0, value: 2048 }], min: 1024, max: 2048 },
  ...over,
});

describe("TileCard", () => {
  it("widens the range to take in the current value", () => {
    render(<TileCard t={tile({ value: 4096 })} windows={[]} start={0} end={1} />);
    const f = (v: number) => fmtValue(v, "bytes_per_second");
    expect(screen.getByRole("img", { name: new RegExp(`min ${f(1024)}, max ${f(4096)}`.replace(/\//g, "\\/")) })).toBeInTheDocument();
    expect(f(4096)).toContain("/s");
  });
  it("formats the range of every unit with the unit's formatter", () => {
    render(<TileCard t={tile({ value: 1500, unit: "percent", series: { metric: "m", unit: "percent", stepMs: 1, points: [], min: 10, max: 2000 } })} windows={[]} start={0} end={1} />);
    expect(screen.getByRole("img", { name: /min 10%, max .*%/ })).toBeInTheDocument();
  });
});

describe("bytes per second", () => {
  it("formats as bytes with /s", () => {
    expect(fmtValue(2048, "bytes_per_second")).toBe("2 KiB/s");
  });
});

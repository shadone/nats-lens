import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { bucketLabel } from "../lib/timeRange";
import { FleetStrip, StripLegend } from "./FleetCard";

describe("readable strip intervals", () => {
  it.each([
    [1_801_000, "30 min"],
    [1_830_000, "30.5 min"],
    [3_600_001, "1 h"],
    [172_801_000, "2 days"],
    [30_000, "30 s"],
    [500, "0.5 s"],
  ])("formats %d ms as %s", (ms, text) => {
    expect(bucketLabel(ms)).toBe(text);
  });

  it("uses the same readable interval in the Fleet legend and accessible strip", () => {
    render(<><StripLegend used={new Set(["ok"])} bucketMs={1_801_000} /><FleetStrip cells={["ok"]} start={0} bucketMs={1_801_000} /></>);
    expect(screen.getByText("Strip, per 30 min:")).toBeInTheDocument();
    expect(screen.getByRole("img", { name: /^Status per 30 min:/ })).toBeInTheDocument();
  });
});

import { describe, expect, it } from "vitest";
import { createRangeDateEditor } from "./rangeDateEditor";

const at = (s: string) => Date.parse(`${s}Z`);
describe("browser locale range editor", () => {
  it.each(["en-US", "en-GB", "nb-NO", "ar-EG", "fa-IR", "ja-JP", "en-US-u-hc-h11", "en-GB-u-hc-h24"])("round trips UTC with %s date order, digits and hour cycle", (locale) => {
    const editor = createRangeDateEditor([locale]);
    for (const time of ["2024-02-29T00:00:00", "2026-10-11T12:30:00", "2026-11-10T23:59:00"]) {
      expect(editor.parse(editor.format(at(time)))).toBe(at(time));
    }
  });
  it("never guesses the opposite date order", () => {
    expect(createRangeDateEditor(["en-US"]).parse("10/11/2026, 2:30 PM")).toBe(at("2026-10-11T14:30:00"));
    expect(createRangeDateEditor(["en-GB"]).parse("10/11/2026, 14:30")).toBe(at("2026-11-10T14:30:00"));
    expect(createRangeDateEditor(["en-GB"]).parse("10/16/2026, 14:30")).toBeNaN();
  });
  it.each(["2/29/2025, 2:30 PM", "4/31/2026, 2:30 PM", "13/1/2026, 2:30 PM", "0/1/2026, 2:30 PM", "1/0/2026, 2:30 PM", "1/1/2026, 0:30 PM", "1/1/2026, 13:30 PM", "1/1/2026, 2:60 PM", "1/1/2026, 2:30", "1/1/26, 2:30 PM", "1/1/0000, 2:30 PM", "", "2026-10-16T14:30", "1/1/2026, 2:30 PM extra", "x".repeat(81)])("rejects invalid or ambiguous input %s", (value) => {
    expect(createRangeDateEditor(["en-US"]).parse(value)).toBeNaN();
  });
  it("validates 24-hour time and Gregorian days, without Date.parse rollover", () => {
    const editor = createRangeDateEditor(["nb-NO"]);
    expect(editor.parse("29.2.2024, 0:30")).toBe(at("2024-02-29T00:30:00"));
    for (const value of ["29.2.2025, 0:30", "31.4.2026, 14:30", "1.1.2026, 24:00", "1.1.2026, -1:00"]) expect(editor.parse(value)).toBeNaN();
  });
  it("handles midnight and noon distinctly and accepts ordinary whitespace", () => {
    const editor = createRangeDateEditor(["en-US"]);
    expect(editor.parse("10/16/2026, 12:30 am")).toBe(at("2026-10-16T00:30:00"));
    expect(editor.parse("10/16/2026, 12:30 PM")).toBe(at("2026-10-16T12:30:00"));
  });
});

import { describe, expect, it } from "vitest";
import { railCount } from "./railCount";

describe("railCount", () => {
  it("keeps counts up to 99 as they are", () => {
    expect(railCount(0)).toBe("0");
    expect(railCount(7)).toBe("7");
    expect(railCount(99)).toBe("99");
  });
  it("shortens counts over 99 to 99+ so the badge does not cover the icon", () => {
    expect(railCount(100)).toBe("99+");
    expect(railCount(1176)).toBe("99+");
  });
});

import { describe, expect, it } from "vitest";
import { ASK_MAX, askText, checkQuestion, withoutAsk } from "./askQuestion";

describe("askText", () => {
  it("reads the rest of the text after a leading ask:, trimmed and as typed", () => {
    expect(askText("ask: why is order-jobs lagging?")).toBe("why is order-jobs lagging?");
    expect(askText("  ASK:why")).toBe("why");
    expect(askText("ask:")).toBe("");
  });

  it("keeps other prefixes as part of the question", () => {
    expect(askText("ask: stream: order since:2h  what  changed")).toBe("stream: order since:2h  what  changed");
  });

  it("is not a question when ask: is not first", () => {
    expect(askText("order ask: why")).toBeUndefined();
    expect(askText("asking")).toBeUndefined();
    expect(askText("task: x")).toBeUndefined();
  });

  it("removes only the prefix for the chip", () => {
    expect(withoutAsk("ask: why lag")).toBe("why lag");
    expect(withoutAsk("order")).toBe("order");
  });
});

describe("checkQuestion", () => {
  it("trims the question", () => {
    expect(checkQuestion("  why lag?  ")).toEqual({ question: "why lag?" });
  });

  it("refuses an empty question", () => {
    expect(checkQuestion("")).toEqual({ problem: "empty", length: 0 });
    expect(checkQuestion("   ")).toEqual({ problem: "empty", length: 0 });
  });

  it("takes at most ASK_MAX characters, counted after trimming", () => {
    expect(ASK_MAX).toBe(2000);
    expect(checkQuestion(` ${"x".repeat(ASK_MAX)} `)).toEqual({ question: "x".repeat(ASK_MAX) });
    expect(checkQuestion("x".repeat(ASK_MAX + 1))).toEqual({ problem: "long", length: ASK_MAX + 1 });
  });

  it("counts a character outside the basic plane once", () => {
    expect(checkQuestion("\u{1F600}".repeat(ASK_MAX)).question).toBeDefined();
  });
});

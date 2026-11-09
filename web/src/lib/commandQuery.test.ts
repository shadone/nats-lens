import { describe, expect, it } from "vitest";
import { narrowedTo, parseCommand, withoutPrefix } from "./commandQuery";

describe("parseCommand", () => {
  it("leaves plain text alone, spaces collapsed", () => {
    expect(parseCommand("  order   jobs ")).toEqual({ text: "order jobs" });
  });

  it("narrows to a kind, with the text after the colon or after a space", () => {
    expect(parseCommand("stream:order")).toEqual({ text: "order", kind: "stream" });
    expect(parseCommand("stream: order")).toEqual({ text: "order", kind: "stream" });
    expect(parseCommand("KV:config")).toEqual({ text: "config", kind: "kv" });
    for (const p of ["server", "consumer", "object", "account", "finding", "event"]) {
      expect(parseCommand(`${p}:x`).kind).toBe(p);
    }
  });

  it("takes the last kind prefix", () => {
    expect(parseCommand("stream: server:nats")).toEqual({ text: "nats", kind: "server" });
  });

  it("reads since: in minutes, hours and days", () => {
    expect(parseCommand("since:30m")).toMatchObject({ text: "", sinceMs: 1_800_000, sinceLabel: "30m" });
    expect(parseCommand("since:2H event:")).toMatchObject({ text: "", kind: "event", sinceMs: 7_200_000, sinceLabel: "2h" });
    expect(parseCommand("nats-1 since:1d").sinceMs).toBe(86_400_000);
  });

  it("keeps an unknown prefix or a bad since: as text", () => {
    expect(parseCommand("subject:orders.>")).toEqual({ text: "subject:orders.>" });
    expect(parseCommand("since:soon")).toEqual({ text: "since:soon" });
    expect(parseCommand("since:0h")).toEqual({ text: "since:0h" });
    expect(parseCommand("since:31d")).toEqual({ text: "since:31d" });
    expect(parseCommand("http://x")).toEqual({ text: "http://x" });
  });
});

describe("withoutPrefix and narrowedTo", () => {
  it("removes one prefix and keeps the rest", () => {
    expect(withoutPrefix("stream:order since:2h", "kind")).toBe("order since:2h");
    expect(withoutPrefix("event: since:2h nats", "since")).toBe("event: nats");
    expect(withoutPrefix("since:soon", "since")).toBe("since:soon");
  });

  it("narrows to a kind, replacing another kind prefix", () => {
    expect(narrowedTo("order", "stream")).toBe("stream: order");
    expect(narrowedTo("server:nats since:2h", "event")).toBe("event: nats since:2h");
    expect(narrowedTo("", "finding")).toBe("finding: ");
  });
});

import { describe, expect, it } from "vitest";
import { A, B, rgone, rreplay, rstep, server } from "../test/fixtures/replay";
import { logFileName, replayLog } from "./replayLog";

const CTX = {
  system: "example-global",
  origin: "https://lens.example.com",
  global: "?from=2026-10-17T06:00:00Z&to=2026-10-17T12:00:00Z&system=example-global",
  link: "https://lens.example.com/replay?from=2026-10-17T06:00:00Z&to=2026-10-17T12:00:00Z&system=example-global&a=2026-10-17T09:14:00Z&b=2026-10-17T09:50:00Z",
};

function story() {
  return rreplay({
    steps: [
      rstep("restart:1", "09:18:00", {
        kind: "restart",
        end: Date.parse("2026-10-17T09:18:30Z"),
        title: "nats-1 restarts",
        body: "Back at 09:18:30 with 4 streams and 9 consumers.",
        evidence: [
          { kind: "event", id: "e 7", time: Date.parse("2026-10-17T09:17:58Z"), label: "pod nats-1 Killing" },
          { kind: "collection", id: "120", time: Date.parse("2026-10-17T09:18:30Z"), label: "collection of 09:18:30", entity: server("nats-1") },
        ],
        evidenceCount: 3,
      }),
      rstep("gap:1", "09:30:00", { kind: "gap", end: Date.parse("2026-10-17T09:33:00Z"), title: "No collections for 3 min", gapKind: "collections" }),
      rstep("finding:2", "09:40:00", { severity: "critical", title: "Consumer missing", evidence: [{ kind: "finding", id: "consumer-013:APP/x", label: "consumer-013 (firing)" }], evidenceCount: 1 }),
      rstep("resolved:3", "10:10:00", { kind: "resolved", title: "Consumer missing resolved", outsideRange: true }),
    ],
    counts: { serversRestarted: 1, leadersMoved: 2, leaderElections: 5 },
    gone: [rgone()],
    completeUntil: Date.parse("2026-10-17T09:45:00Z"),
  });
}

describe("replayLog", () => {
  const text = replayLog(story(), CTX);

  it("starts with the system, A, B, the sources and the link", () => {
    expect(text.split("\n").slice(0, 8)).toEqual([
      "nats-lens replay",
      "System: example-global",
      "A: 2026-10-17 09:14:00 UTC",
      "B: 2026-10-17 09:50:00 UTC",
      "Collection at A: 2026-10-17 09:13:50 UTC (epoch 100)",
      "Collection at B: 2026-10-17 09:49:50 UTC (epoch 172)",
      "Built from collections, NATS advisories and Kubernetes events; every line links to its evidence.",
      `Link: ${CTX.link}`,
    ]);
  });

  it("writes each step with its time, kind, body and evidence as absolute links", () => {
    expect(text).toContain("09:18:00–09:18:30  Restart  nats-1 restarts\n    Back at 09:18:30 with 4 streams and 9 consumers.\n");
    expect(text).toContain("    event: pod nats-1 Killing https://lens.example.com/events?id=e+7&from=2026-10-17T06%3A00%3A00Z&to=2026-10-17T12%3A00%3A00Z&system=example-global\n");
    // A collection opens its entity over the hour that ends a second after it.
    expect(text).toContain("    collection: collection of 09:18:30 https://lens.example.com/servers/nats-1?from=2026-10-17T08%3A18%3A30Z&to=2026-10-17T09%3A18%3A31Z&system=example-global\n");
    expect(text).toContain("    and 1 more\n");
    expect(text).toContain("09:40:00  Finding (critical)  Consumer missing\n    finding: consumer-013 (firing) https://lens.example.com/findings/consumer-013%3AAPP%2Fx?");
    expect(text).toContain("Steps after 09:45 may be missing: the read limit was reached.");
  });

  it("puts the steps after B under their own heading, then the counts, the gone assets and the gaps", () => {
    const order = ["What happened, in order", "After B", "Between A and B", "Gone between A and B", "Gaps"].map((h) => text.indexOf(`\n${h}\n`));
    expect(order.every((i, k) => i > 0 && (k === 0 || i > order[k - 1]))).toBe(true);
    expect(text).toContain("10:10:00  Resolved  Consumer missing resolved  [outside the range]");
    expect(text).toContain("Servers restarted: 1\n");
    expect(text).toContain("Leaders moved: 2 (of 5 elections)\n");
    expect(text).toContain("Servers stopped: not observed\n");
    expect(text).toContain("restock-alerts (consumer) https://lens.example.com/consumers/APP/inventory-changes/restock-alerts?from=2026-10-17T08%3A14%3A00Z&to=2026-10-17T09%3A14%3A01Z&system=example-global");
    expect(text).toContain("    at A: 3 servers: nats-0, nats-1, nats-2; at B: none\n    delete event: none captured\n");
    expect(text.endsWith("Gaps\n\n09:30:00–09:33:00  No collections for 3 min\n")).toBe(true);
  });

  it("names the file by system and moments", () => {
    expect(logFileName("example-global", { a: A, b: B })).toBe("replay-example-global-20261017T091400Z-20261017T095000Z.txt");
    expect(logFileName("", { a: A, b: B })).toBe("replay-default-20261017T091400Z-20261017T095000Z.txt");
  });
});

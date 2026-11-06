import { describe, expect, it } from "vitest";
import { assetStatus, contextText, headline, rightNow, storyNote, safePlaybook, remediationParts } from "./onCallView";
import type { Finding } from "../api/model";
const f = { checkName: "Consumer quorum lost", entity: { kind: "consumer", name: "worker", consumer: "worker", stream: "order-jobs" } } as Finding;
describe("on-call words follow observed facts", () => {
  it("names one entity and counts a group", () => {
    expect(headline(f, 1)).toBe("Consumer quorum lost: worker on order-jobs");
    expect(headline(f, 4)).toBe("4 consumers: Consumer quorum lost");
  });
  it("never turns missing facts into zero", () => {
    expect(rightNow({})).toBe("Present observations are not reported.");
    expect(rightNow({ serversExpected: 3 })).not.toContain("0");
    expect(rightNow({ serversExpected: 3, serversAnswering: 0, deliveredPerSecond: 0, streamMsgsPerSecond: 0 })).toBe("0 of 3 servers answer; reporting consumers deliver 0 msgs/s; reporting streams receive 0 msgs/s.");
    expect(rightNow({ serversAnswering: 2 })).toBe("2 servers answer.");
  });
  it("uses only reported placement and answering state", () => {
    expect(assetStatus({})).toBe("Placement not reported");
    expect(assetStatus({ replicas: 3 })).toBe("Online count not reported · 3 configured replicas");
    expect(assetStatus({ replicas: 3, online: 0, leader: "nats-1" })).toBe("0 of 3 online · leader nats-1");
    expect(assetStatus({ answering: false })).toBe("Not answering");
    expect(assetStatus({ answering: true })).toBe("Answering");
  });
  it("preserves story limits, no history and context without inventing a cause", () => {
    expect(storyNote(2, 2, "")).toBe("");
    expect(storyNote(12, 18, "History is incomplete.")).toBe("Showing 12 of 18 steps. History is incomplete.");
    expect(storyNote(0, 0, "Before stored history.")).toBe("Before stored history.");
    expect(contextText(undefined)).toBe("");
    expect(contextText({ label: "GKE node upgrade" })).toBe("Observed Kubernetes window: GKE node upgrade");
  });
  it("only links an https playbook", () => {
    expect(safePlaybook("javascript:alert(1)")).toBeUndefined();
    expect(safePlaybook("https://runbooks.example.com/nats")).toBe("https://runbooks.example.com/nats");
  });
});

it("separates the remediation warning from subsequent steps without cutting inline code", () => {
  expect(remediationParts("Do not restart first. Recreate with `nats consumer add`. Then check delivery.")).toEqual({ warning: "Do not restart first.", steps: ["Recreate with `nats consumer add`.", "Then check delivery."] });
  expect(remediationParts("Inspect `config.v2` first")).toEqual({ warning: "Inspect `config.v2` first", steps: [] });
  expect(remediationParts("")).toEqual({ warning: "", steps: [] });
});

it("describes a recent event window without asserting an active upgrade", () => {
  expect(rightNow({ openWindow: { start: 1, end: 2, label: "GKE node upgrade", detail: "" } })).toBe("recent Kubernetes events: GKE node upgrade.");
});

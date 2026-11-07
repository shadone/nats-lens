import { describe, expect, it } from "vitest";
import type { Entity, Finding } from "../api/model";
import { groupCauses } from "./causeGroups";

const acc = (k: string): Entity => ({ kind: "account", key: k, name: k, account: k, stream: "", consumer: "", server: "" });
const srv = (k: string): Entity => ({ kind: "server", key: k, name: k, account: "", stream: "", consumer: "", server: k });
function f(id: string, code: string, e: Entity, extra: Partial<Finding>): Finding {
  return { id, checkCode: code, checkName: code, severity: "warning", state: "firing", entity: e, detail: id, remediation: "", relatedConfig: [], relatedConfigSource: "", transitionsLastHour: 0, ...extra };
}

// A reviewer's cases for the order of the cause cards.
describe("cause card rank", () => {
  it("ranks a card mixing an expiry and a non-expiry warning with the non-expiry cards, ahead of a larger expiry-only card", () => {
    const cause = { id: "ev1", kind: "server.shutdown", time: 0, summary: "s" } as unknown as Finding["cause"];
    const { groups } = groupCauses([
      f("e1", "account-004", acc("A1"), { expiry: true }),
      f("e2", "account-004", acc("A2"), { expiry: true }),
      f("e3", "account-004", acc("A3"), { expiry: true }),
      f("m1", "server-016", srv("nats-0"), { expiry: true, cause }),
      f("m2", "server-001", srv("nats-1"), { cause }),
      f("i1", "server-005", srv("nats-2"), { severity: "info" }),
      f("c1", "server-016", srv("nats-3"), { severity: "critical", expiry: true }),
      f("mu", "server-002", srv("nats-4"), { severity: "critical", mute: { id: "1" } as unknown as Finding["mute"] }),
    ]);
    expect(groups.map((g) => g.key)).toEqual(["check:server-016|system", "cause:ev1", "check:account-004|system", "check:server-005|system"]);
  });

  it("keeps the input order for ties", () => {
    const { groups } = groupCauses([
      f("x", "account-003", acc("X"), { expiry: true }),
      f("y", "server-016", srv("nats-0"), { expiry: true }),
    ]);
    expect(groups.map((g) => g.checkCodes[0])).toEqual(["account-003", "server-016"]);
  });
});

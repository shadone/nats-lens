import { describe, expect, it } from "vitest";
import type { Entity, Finding } from "../api/model";
import type { AssetPlacement } from "../api/overview";
import { acceptable, cliCheck, evidenceEntities, placementSummary, serverRows } from "./causeEvidence";
import { groupCauses } from "./causeGroups";

const ent = (kind: Entity["kind"], key: string, over: Partial<Entity> = {}): Entity => {
  const [a, s, c] = key.split("/");
  const e: Entity = { kind, key, name: key, account: "", stream: "", consumer: "", server: "" };
  if (kind === "stream") Object.assign(e, { account: a, stream: s, name: s });
  if (kind === "consumer") Object.assign(e, { account: a, stream: s, consumer: c, name: c });
  if (kind === "account") e.account = key;
  if (kind === "server") e.server = key;
  return { ...e, ...over };
};
const consumer = (n: string, stream = "order-jobs") => ent("consumer", `APP/${stream}/${n}`);
function f(id: string, e: Entity | undefined, extra: Partial<Finding> = {}): Finding {
  return { id, checkCode: "consumer-009", checkName: "x", severity: "warning", state: "firing", entity: e, detail: "", remediation: "", relatedConfig: [], relatedConfigSource: "", transitionsLastHour: 0, ...extra };
}
const led = (leader: string, ...rest: [string, AssetPlacement["replicas"][number]["state"]][]): AssetPlacement => ({
  reported: true,
  leader,
  replicas: [{ server: leader, state: "leader" }, ...rest.map(([server, state]) => ({ server, state }))],
});

describe("serverRows", () => {
  it("puts every consumer on nats-eu-3 first when it leads them all", () => {
    const fs = ["a", "b", "c", "d"].map((n) => f(n, consumer(n)));
    const p = new Map(fs.map((x, i) => [x.entity!.key, led("nats-eu-3", [i % 2 ? "nats-eu-1" : "nats-eu-2", "in_sync"])]));
    const { rows, unplaced } = serverRows(fs, p);
    expect(rows.map((r) => [r.server, r.assets.length, r.leads])).toEqual([
      ["nats-eu-3", 4, 4],
      ["nats-eu-1", 2, 0],
      ["nats-eu-2", 2, 0],
    ]);
    expect(unplaced).toEqual([]);
    expect(placementSummary(rows, 4, "consumers")).toBe("nats-eu-3 leads 4 of 4 consumers.");
  });

  it("names the server whose replicas are offline before the leaders", () => {
    const fs = [f("p", ent("stream", "APP/price-changes")), f("s", ent("stream", "APP/shipping-updates"))];
    const p = new Map([
      ["APP/price-changes", led("nats-us-1", ["nats-us-2", "in_sync"], ["nats-us-4", "offline"])],
      ["APP/shipping-updates", led("nats-us-2", ["nats-us-3", "in_sync"], ["nats-us-4", "offline"])],
    ]);
    const { rows } = serverRows(fs, p);
    expect(rows[0].server).toBe("nats-us-4");
    expect(placementSummary(rows, 2, "streams")).toBe("nats-us-4 holds a replica of 2 of 2 streams, offline.");
  });

  it("says when one server holds every asset while another holds fewer, and nothing for one asset or an even spread", () => {
    const fs = ["a", "b", "c"].map((n) => f(n, consumer(n)));
    const p = new Map([
      [fs[0].entity!.key, led("nats-0", ["nats-1", "in_sync"])],
      [fs[1].entity!.key, led("nats-2", ["nats-1", "in_sync"])],
      [fs[2].entity!.key, led("nats-3", ["nats-1", "in_sync"])],
    ]);
    expect(placementSummary(serverRows(fs, p).rows, 3, "consumers")).toBe("All 3 consumers have a replica on nats-1.");
    expect(placementSummary(serverRows(fs.slice(0, 1), p).rows, 1, "consumer")).toBeUndefined();
    const even = new Map(fs.map((x) => [x.entity!.key, led("nats-0", ["nats-1", "in_sync"])]));
    // nats-0 leads all three: that stands out.
    expect(placementSummary(serverRows(fs, even).rows, 3, "consumers")).toBe("nats-0 leads 3 of 3 consumers.");
  });

  it("lists a stream no server holds apart, a server finding on its server and an account on none", () => {
    const fs = [f("g", ent("stream", "APP/gone")), f("s", ent("server", "nats-2")), f("a", ent("account", "APP")), f("c", ent("connection", "nats-1/42", { server: "nats-1" }))];
    const { rows, unplaced } = serverRows(fs, new Map([["APP/gone", { reported: false, leader: "", replicas: [] }]]));
    expect(unplaced.map((x) => x.id)).toEqual(["g"]);
    expect(rows.map((r) => `${r.server}:${r.assets[0].state}`).sort()).toEqual(["nats-1:connection", "nats-2:server"]);
  });
});

describe("evidenceEntities", () => {
  it("asks once for each stream and consumer of the listed entities only, at most the limit", () => {
    const fs = [...Array.from({ length: 10 }, (_, i) => f(`c${i}`, consumer(`w${i}`))), f("dup", consumer("w0"), { checkCode: "consumer-002" }), f("s", ent("server", "nats-0"))];
    const es = evidenceEntities(groupCauses(fs).groups, 200);
    // The consumer-009 card lists 8 of its 10; consumer-002 repeats w0; the server has no placement.
    expect(es.map((e) => e.name)).toEqual(["w0", "w1", "w2", "w3", "w4", "w5", "w6", "w7"]);
    expect(evidenceEntities(groupCauses(fs).groups, 3)).toHaveLength(3);
  });
});

describe("cliCheck", () => {
  it("derives the command from the entities and never names credentials", () => {
    expect(cliCheck([f("a", consumer("resize-worker"))], "")).toEqual({
      command: "nats consumer info --json -- order-jobs resize-worker",
      note: "Run with a nats context for account APP on the default system.",
    });
    expect(cliCheck([f("a", consumer("a")), f("b", consumer("b"))], "example-prod")?.command).toBe("nats consumer report -- order-jobs");
    expect(cliCheck([f("a", consumer("a")), f("b", consumer("b", "orders"))], "")?.command).toBe("nats stream report");
    expect(cliCheck([f("s", ent("stream", "APP/price-changes", { accountName: "Shop" }))], "x")).toEqual({
      command: "nats stream info --json -- price-changes",
      note: "Run with a nats context for account Shop (APP) on x.",
    });
    expect(cliCheck([f("s", ent("server", "nats-2"))], "")).toEqual({ command: "nats server info -- nats-2", note: "Run with a nats context for the system account on the default system." });
    expect(cliCheck([f("s", ent("server", "nats-2")), f("t", ent("server", "nats-3"))], "")?.command).toBe("nats server list");
    expect(cliCheck([f("a", ent("account", "APP"))], "")?.command).toBe("nats account info");
    expect(cliCheck([f("c", ent("client", "callback-dispatcher"))], "")?.command).toBe("nats server report connections");
    expect(cliCheck([f("y", undefined)], "")?.command).toBe("nats server report jetstream");
  });

  it("offers none for entities of several kinds or accounts, and no command for a name a shell would misread", () => {
    expect(cliCheck([f("a", consumer("a")), f("s", ent("server", "nats-0"))], "")).toBeUndefined();
    expect(cliCheck([f("a", ent("account", "APP")), f("b", ent("account", "SUPPLIERS"))], "")).toBeUndefined();
    expect(cliCheck([f("a", consumer("bad\nname"))], "")?.command).toBeUndefined();
  });
});

describe("acceptable", () => {
  it("offers acceptance for a warning or info on one entity, never a critical, the system or a muted finding", () => {
    expect(acceptable(f("w", consumer("a")))).toBe(true);
    expect(acceptable(f("i", ent("server", "nats-0"), { severity: "info" }))).toBe(true);
    expect(acceptable(f("c", consumer("a"), { severity: "critical" }))).toBe(false);
    expect(acceptable(f("s", undefined))).toBe(false);
    expect(acceptable(f("s", ent("system", "")))).toBe(false);
    expect(acceptable(f("m", consumer("a"), { mute: { id: "1" } as unknown as Finding["mute"] }))).toBe(false);
  });
});

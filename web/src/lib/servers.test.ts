import { fromJson } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { serverList, type ServerRow } from "../api/servers";
import { ListServersResponseSchema } from "../gen/natslens/v1/servers_pb";
import { exampleGlobalServers } from "../test/fixtures/exampleGlobal";
import { colourScale } from "./serverColour";
import { attentionReasons, groupByCluster, isFlat, majorityVersion, median, needsAttention, sortProblemsFirst, storePct } from "./servers";

const list = serverList(fromJson(ListServersResponseSchema, exampleGlobalServers));
const byName = (n: string) => list.servers.find((s) => s.name === n)!;

describe("example-global servers", () => {
  it("has three clusters", () => {
    expect(groupByCluster(list.servers).map((g) => g.cluster)).toEqual(["us", "ap", "eu"]);
  });

  it("puts the down server first in its group", () => {
    const us = groupByCluster(list.servers).find((g) => g.cluster === "us")!;
    expect(us.rows[0].name).toBe("nats-us-4");
  });

  it("names the majority version", () => {
    expect(majorityVersion(list.servers)).toBe("2.12.4");
  });

  it("lists the servers that need attention by the rule", () => {
    const names = needsAttention(list.servers).map((s) => s.name);
    expect(names.sort()).toEqual(["nats-ap-2", "nats-ap-4", "nats-ap-5", "nats-us-3", "nats-us-4"]);
  });

  it("says why a server needs attention", () => {
    const v = majorityVersion(list.servers);
    expect(attentionReasons(byName("nats-us-4"), v)).toContain("did not answer");
    expect(attentionReasons(byName("nats-ap-2"), v)).toContain("store 71%");
    expect(attentionReasons(byName("nats-ap-4"), v)).toContain("version 2.12.2");
    expect(attentionReasons(byName("nats-eu-0"), v)).toEqual([]);
  });

  it("does not count a muted finding as a reason", () => {
    const muted = { ...byName("nats-eu-0"), status: "muted" as const };
    expect(attentionReasons(muted, "2.12.4")).toEqual([]);
  });

  it("colours the 71% store in the top band", () => {
    const scale = colourScale("store", list.servers);
    expect(scale.binOf(byName("nats-ap-2"))?.cls).toBe("heat-4");
    expect(scale.binOf(byName("nats-eu-0"))?.cls).toBe("heat-3");
  });
});

describe("one cluster", () => {
  it("is flat, and its one group keeps the cluster's name", () => {
    const one = list.servers.filter((s) => s.cluster === "eu");
    expect(isFlat(one)).toBe(true);
    expect(isFlat(list.servers)).toBe(false);
    expect(groupByCluster(one).map((g) => g.cluster)).toEqual(["eu"]);
  });
});

describe("values not observed", () => {
  const bare: ServerRow = { ...byName("nats-eu-0"), cpuPercent: undefined, jsStorageBytes: undefined };

  it("gives no colour and a dash for a missing CPU", () => {
    const scale = colourScale("cpu", [bare]);
    expect(scale.binOf(bare)).toBeUndefined();
    expect(scale.valueText(bare)).toBe("–");
  });

  it("has no store percentage without storage", () => {
    expect(storePct(bare)).toBeUndefined();
  });

  it("has no median of nothing", () => {
    expect(median([])).toBeUndefined();
    expect(median([3, 1, 2])).toBe(2);
    expect(median([1, 2, 3, 4])).toBe(2.5);
  });
});

describe("problems first", () => {
  it("orders a down server before a warning and both before ok ones", () => {
    const order = sortProblemsFirst(list.servers.filter((s) => s.cluster === "us"), "2.12.4").map((s) => s.name);
    expect(order.slice(0, 3)).toEqual(["nats-us-4", "nats-us-3", "nats-us-0"]);
  });
});

describe("map colours", () => {
  it("puts a value shown as 60% in the 60% or more bin", () => {
    const s = { ...byName("nats-eu-0"), cpuPercent: 59.6 };
    const scale = colourScale("cpu", [s]);
    expect(scale.valueText(s)).toBe("60%");
    expect(scale.binOf(s)?.label).toBe("60% or more");
  });

  it("keeps three version colours, then other versions", () => {
    const vs = ["2.12.4", "2.12.4", "2.12.4", "2.12.3", "2.12.3", "2.12.2", "2.11.9", "2.10.1"];
    const rows = vs.map((version, i) => ({ ...byName("nats-eu-0"), name: `n-${i}`, version }));
    const scale = colourScale("version", rows);
    expect(scale.bins.map((b) => [b.cls, b.label])).toEqual([
      ["ver-0", "2.12.4"],
      ["ver-1", "2.12.3"],
      ["ver-2", "2.12.2"],
      ["ver-3", "other versions"],
    ]);
    expect(scale.binOf(rows[6])?.label).toBe("other versions");
    expect(scale.binOf(rows[7])?.label).toBe("other versions");
  });

  it("scales from the servers that answered only", () => {
    const present = list.servers.filter((s) => s.present);
    const withStale = [...present, { ...byName("nats-us-4"), connections: 10_000 }];
    expect(colourScale("connections", withStale).bins).toEqual(colourScale("connections", present).bins);
  });
});

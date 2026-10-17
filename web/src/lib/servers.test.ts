import { fromJson } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { serverList, type ServerRow } from "../api/servers";
import { ListServersResponseSchema } from "../gen/natslens/v1/servers_pb";
import { exampleGlobalServers } from "../test/fixtures/exampleGlobal";
import { colourScale } from "./serverColour";
import { attentionReasons, groupByCluster, majorityVersion, median, needsAttention, sortProblemsFirst, storePct } from "./servers";

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
  it("gives a single group without a name", () => {
    const one = list.servers.filter((s) => s.cluster === "eu");
    const groups = groupByCluster(one);
    expect(groups).toHaveLength(1);
    expect(groups[0].cluster).toBe("");
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

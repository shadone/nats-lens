import { describe, expect, it } from "vitest";
import { globalQuery, withGlobal } from "./globalQuery";

const live6h = { kind: "live", preset: "6h" } as const;
const live24h = { kind: "live", preset: "24h" } as const;

describe("globalQuery", () => {
  it("writes the range, the system and the account", () => {
    expect(globalQuery(live6h, "example-global", "APP")).toBe("?range=6h&system=example-global&account=APP");
  });

  it("leaves out what is unset", () => {
    expect(globalQuery(live24h, "", "")).toBe("");
    expect(globalQuery(live24h, "example-prod", "")).toBe("?system=example-prod");
  });

  it("writes a fixed range with both ends", () => {
    const q = globalQuery({ kind: "fixed", from: Date.parse("2026-10-17T09:00:00Z"), to: Date.parse("2026-10-17T10:00:00Z") }, "a", "");
    expect(q).toBe("?from=2026-10-17T09%3A00%3A00Z&to=2026-10-17T10%3A00%3A00Z&system=a");
  });
});

describe("withGlobal", () => {
  it("adds the global parameters to a link", () => {
    expect(withGlobal("/servers", "?range=6h&system=a")).toBe("/servers?range=6h&system=a");
    expect(withGlobal("/connections?server=nats-1", "?system=a")).toBe("/connections?server=nats-1&system=a");
  });

  it("keeps a parameter the link sets itself", () => {
    expect(withGlobal("/?system=b", "?range=6h&system=a")).toBe("/?system=b&range=6h");
    expect(withGlobal("/consumers?account=OPS", "?account=APP")).toBe("/consumers?account=OPS");
  });

  it("drops the account scope on a link to another system", () => {
    expect(withGlobal("/?system=b", "?range=6h&system=a&account=APP")).toBe("/?system=b&range=6h");
    expect(withGlobal("/consumers?system=a", "?system=a&account=APP")).toBe("/consumers?system=a&account=APP");
  });

  it("carries the account scope only to the pages that apply it, and their detail pages", () => {
    const g = "?system=a&account=APP";
    expect(withGlobal("/consumers", g)).toBe("/consumers?system=a&account=APP");
    expect(withGlobal("/connections?server=nats-1", g)).toBe("/connections?server=nats-1&system=a&account=APP");
    expect(withGlobal("/consumers/APP/order-jobs/resize-worker", g)).toBe("/consumers/APP/order-jobs/resize-worker?system=a&account=APP");
    for (const to of ["/servers", "/servers/nats-1", "/servers/slow-consumers"]) expect(withGlobal(to, g)).toBe(`${to}?system=a&account=APP`);
    // The Overview applies it to its findings and lists; its Timeline pages read the whole system.
    expect(withGlobal("/", g)).toBe("/?system=a&account=APP");
    expect(withGlobal("/findings?check=consumer-009", g)).toBe("/findings?check=consumer-009&system=a&account=APP");
    for (const to of ["/timeline/events", "/streams", "/events", "/fleet"]) expect(withGlobal(to, g)).toBe(`${to}?system=a`);
  });

  it("leaves a link alone without global parameters", () => {
    expect(withGlobal("/streams", "")).toBe("/streams");
  });
});


it("carries reference account context without leaking it to another system", () => {
  expect(withGlobal("/checks/consumer-009","?range=6h&system=a&account=APP")).toBe("/checks/consumer-009?range=6h&system=a&account=APP");
  expect(withGlobal("/checks?system=b","?range=6h&system=a&account=APP")).toBe("/checks?system=b&range=6h");
});

it("keeps explicit fragments after global and own query parameters", () => {
  expect(withGlobal("/checks/server-014#evidence", "?range=6h&system=a&account=APP"))
    .toBe("/checks/server-014?range=6h&system=a&account=APP#evidence");
  expect(withGlobal("/checks?system=b#evidence", "?range=6h&system=a&account=APP"))
    .toBe("/checks?system=b&range=6h#evidence");
  expect(withGlobal("/checks#evidence", "")).toBe("/checks#evidence");
});

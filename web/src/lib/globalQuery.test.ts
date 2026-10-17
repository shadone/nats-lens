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
    expect(withGlobal("/streams?system=a", "?system=a&account=APP")).toBe("/streams?system=a&account=APP");
  });

  it("leaves a link alone without global parameters", () => {
    expect(withGlobal("/streams", "")).toBe("/streams");
  });
});

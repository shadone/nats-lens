// The global query is worked out once per URL change, not once per link.
import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { RLink } from "../components/common";
import { renderApp } from "../test/render";
import * as gq from "./globalQuery";

vi.mock("./globalQuery", async (orig) => {
  const real = await orig<typeof import("./globalQuery")>();
  return { ...real, globalQuery: vi.fn(real.globalQuery) };
});
vi.mock("../api/client", () => ({ lens: { getShellStatus: vi.fn(() => new Promise(() => {})) } }));

describe("global query", () => {
  it("is computed once for a page full of links", () => {
    renderApp(
      <>
        {Array.from({ length: 50 }, (_, i) => (
          <RLink key={i} to={`/servers/nats-${i}`}>
            {`nats-${i}`}
          </RLink>
        ))}
      </>,
      "/servers?system=sys-a&range=6h",
    );
    expect(screen.getAllByRole("link")).toHaveLength(50);
    expect(screen.getByRole("link", { name: "nats-7" })).toHaveAttribute("href", "/servers/nats-7?range=6h&system=sys-a");
    expect(vi.mocked(gq.globalQuery).mock.calls.length).toBeLessThanOrEqual(2);
  });
});

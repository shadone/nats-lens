// Review cases: a failing part of the shell call, and the refetch
// interval's fallback.
import { create } from "@bufbuild/protobuf";
import { screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { GetShellStatusResponseSchema } from "../gen/natslens/v1/systems_pb";
import { renderApp } from "../test/render";
import { errorInfo } from "./errors";
import { useShellStatus } from "./queries";
import { lens } from "./client";
import { shellQuery } from "./shell";
import { useCurrentSystem, useSystemBadges, useSystems } from "./systems";

vi.mock("./client", () => ({ lens: { getShellStatus: vi.fn(), listSystems: vi.fn() } }));
const l = vi.mocked(lens);

function Probe() {
  const systems = useSystems();
  const shell = useShellStatus();
  const badges = useSystemBadges().data;
  const known = useCurrentSystem().known;
  return (
    <div data-testid="probe">
      err={shell.error ? errorInfo(shell.error).code : "none"} systems={systems.data?.systems.length ?? "-"} badges={badges?.length ?? "-"} known={String(known)}
    </div>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  // The server reports the failure of the system's own part beside the systems.
  l.getShellStatus.mockResolvedValue(
    create(GetShellStatusResponseSchema, {
      systems: [{ system: { id: "shop-prod" } }, { system: { id: "shop-staging" } }],
      defaultSystem: "shop-prod",
      systemKnown: true,
      errorCode: "deadline_exceeded",
      error: "context deadline exceeded",
    }) as never,
  );
});

it("keeps the system list and the Fleet badge when the page system's own part fails", async () => {
  renderApp(<Probe />, "/?system=shop-prod");
  await waitFor(() => expect(screen.getByTestId("probe")).toHaveTextContent("err=deadline_exceeded"), { timeout: 10_000 });
  expect(screen.getByTestId("probe")).toHaveTextContent("systems=2 badges=2 known=true");
}, 15_000);

it("falls back to the live interval without a freshness or with a zero interval", () => {
  const f = shellQuery("x").refetchInterval as (q: unknown) => number;
  expect(f({ state: { data: undefined } })).toBe(30_000);
  expect(f({ state: { data: { freshness: undefined } } })).toBe(30_000);
  expect(f({ state: { data: { freshness: { intervalMs: 0 } } } })).toBe(30_000);
  expect(f({ state: { data: { freshness: { intervalMs: 5_000 } } } })).toBe(5_000);
});

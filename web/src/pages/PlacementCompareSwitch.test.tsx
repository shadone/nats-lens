import { act, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useNavigate, type NavigateFunction } from "react-router";
import * as requests from "../api/requests";
import type { RangeSpec } from "../lib/timeRange";
import { masset, mcell, mmatrix } from "../test/fixtures/placement";
import { renderApp } from "../test/render";
import { Placement } from "./Placement";

vi.mock("../api/requests", () => ({ getPlacement: vi.fn(), getLensInfo: vi.fn(() => new Promise(() => {})), getShellStatus: vi.fn(() => new Promise(() => {})) }));

afterEach(() => vi.mocked(requests.getPlacement).mockReset());

const now = () => mmatrix([masset("stream", "orders", "", [mcell("nats-0", "leader"), mcell("nats-1", "in_sync", { lag: 0 })])]);
const before = () => mmatrix([masset("stream", "orders", "", [mcell("nats-0", "leader"), mcell("nats-1", "offline")])]);
const never = () => new Promise<never>(() => {});

let go: NavigateFunction;
function Nav() {
  go = useNavigate();
  return null;
}

function page(route: string) {
  renderApp(
    <>
      <Placement />
      <Nav />
    </>,
    route,
  );
}

const line = () => screen.getByRole("status", { name: "" });

// The comparison of system a must not stand for system b while b's moment
// loads, even after the choice went away and came back.
it("drops the comparison of the previous system until the new moment loads", async () => {
  vi.mocked(requests.getPlacement).mockImplementation(async (sys: string, spec: RangeSpec) =>
    spec.kind === "live" ? now() : sys === "a" ? before() : never(),
  );
  page("/placement?system=a&cmp=1d");
  await waitFor(() => expect(line()).toHaveTextContent("1 cell differs"));
  act(() => go("/placement?system=b"));
  await waitFor(() => expect(vi.mocked(requests.getPlacement).mock.calls.some(([s]) => s === "b")).toBe(true));
  await screen.findAllByText("orders");
  act(() => go("/placement?system=b&cmp=1d"));
  await waitFor(() => expect(line()).toHaveTextContent(/^Loading the matrix of/));
  expect(line()).not.toHaveTextContent(/differ/);
});

// A change from a live range to a fixed one drops the comparison of the
// live moment until the fixed range's moment loads.
it("drops the comparison of the previous range until the new moment loads", async () => {
  const to = Date.parse("2026-09-10T09:00:00Z");
  vi.mocked(requests.getPlacement).mockImplementation(async (_s: string, spec: RangeSpec) =>
    spec.kind === "live" || spec.to === to ? now() : spec.to === to - 86_400_000 ? never() : before(),
  );
  page("/placement?cmp=1d");
  await waitFor(() => expect(line()).toHaveTextContent("1 cell differs"));
  act(() => go("/placement?cmp=1d&from=2026-09-10T08:00:00Z&to=2026-09-10T09:00:00Z"));
  await waitFor(() => expect(line()).toHaveTextContent("Loading the matrix of 9 Sep 09:00 UTC"));
  expect(line()).not.toHaveTextContent(/differ/);
});

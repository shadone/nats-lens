import { act, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import type { RangeSpec } from "../lib/timeRange";
import { masset, mcell, mmatrix } from "../test/fixtures/placement";
import { renderApp } from "../test/render";
import { Placement } from "./Placement";

vi.mock("../api/requests", () => ({ getPlacement: vi.fn(), getLensInfo: vi.fn(() => new Promise(() => {})), getShellStatus: vi.fn(() => new Promise(() => {})) }));

afterEach(() => vi.useRealTimers());

// On a live range with cmp=1d, the moment rolls each minute, the comparison
// line and its dots stay while the next minute's matrix loads.
it("keeps the comparison while the moment rolls to the next minute", async () => {
  vi.useFakeTimers({ toFake: ["setInterval", "clearInterval", "Date"], shouldAdvanceTime: true });
  const m = () => mmatrix([masset("stream", "orders", "", [mcell("nats-0", "leader"), mcell("nats-1", "in_sync", { lag: 0 })])]);
  const y = () => mmatrix([masset("stream", "orders", "", [mcell("nats-0", "leader"), mcell("nats-1", "offline")])]);
  let hang = false;
  vi.mocked(requests.getPlacement).mockImplementation(async (_s: string, spec: RangeSpec) =>
    spec.kind === "fixed" ? (hang ? new Promise(() => {}) : y()) : m(),
  );
  renderApp(<Placement />, "/placement?cmp=1d");
  await waitFor(() => expect(screen.getByRole("status", { name: "" })).toHaveTextContent(/1 cell differs|1 cells differ|cells? differ/));
  hang = true;
  await act(async () => {
    vi.advanceTimersByTime(120_000);
  });
  expect(screen.getByRole("status", { name: "" })).toHaveTextContent(/differ/);
});

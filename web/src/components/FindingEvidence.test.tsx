import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { ConsumerServerView } from "../api/model";
import { renderApp } from "../test/render";
import { CopiesTable, commonCreated, commonPosition, copiesDisagree } from "./FindingEvidence";

const day = 86_400_000;
const t0 = Date.UTC(2026, 9, 8, 9, 34);

const copy = (server: string, extra: Partial<ConsumerServerView> = {}): ConsumerServerView => ({
  server, isLeader: false, current: true, offline: false, deliveredStreamSeq: 1000, ackFloorStreamSeq: 990, created: t0, configHash: "h1", ...extra,
});

function rows() {
  return within(screen.getByRole("table", { name: "Server copies" })).getAllByRole("row").slice(1);
}

describe("the creation time most copies share", () => {
  it("is the one more than half the copies were created near, so the lone old copy stands out", () => {
    expect(commonCreated([t0, t0 + 65 * day, t0 + 65 * day + 60_000])).toBe(t0 + 65 * day);
  });
  it("is unset when no time has more than half the copies", () => {
    expect(commonCreated([t0 + 5 * day, t0])).toBeUndefined();
    expect(commonCreated([t0, t0 + 5 * day])).toBeUndefined();
  });
  it("is unset with no creation time", () => {
    expect(commonCreated([])).toBeUndefined();
  });
});

describe("the position most copies share", () => {
  it("counts a difference of at most max(100, 1%) as the same", () => {
    expect(commonPosition([1000, 1050, 1099])).toBeDefined();
    expect(commonPosition([1_000_000, 1_009_000, 5])).toBeDefined();
  });
  it("is unset when no value has more than half the copies", () => {
    expect(commonPosition([1000, 5000])).toBeUndefined();
  });
});

describe("server copies table", () => {
  it("marks nothing when two copies disagree, in either order, and says so", () => {
    for (const pair of [
      [copy("nats-0", { isLeader: true, created: t0, deliveredStreamSeq: 1000, configHash: "a" }), copy("nats-1", { created: t0 + 5 * day, deliveredStreamSeq: 9000, configHash: "b" })],
      [copy("nats-1", { created: t0 + 5 * day, deliveredStreamSeq: 9000, configHash: "b" }), copy("nats-0", { isLeader: true, created: t0, deliveredStreamSeq: 1000, configHash: "a" })],
    ]) {
      const { unmount } = renderApp(<CopiesTable kind="consumer" copies={pair} />);
      for (const r of rows()) expect(r).not.toHaveClass("odd");
      expect(copiesDisagree("consumer", pair)).toBe(true);
      unmount();
    }
  });

  it("marks a leader ahead of the followers", () => {
    const copies = [copy("nats-0", { isLeader: true, deliveredStreamSeq: 1200 }), copy("nats-1"), copy("nats-2")];
    renderApp(<CopiesTable kind="consumer" copies={copies} />);
    const [leader, f1, f2] = rows();
    expect(leader).toHaveClass("odd");
    expect(within(leader).getByText("differs from most copies")).toBeInTheDocument();
    expect(f1).not.toHaveClass("odd");
    expect(f2).not.toHaveClass("odd");
    expect(copiesDisagree("consumer", copies)).toBe(false);
  });

  it("never takes an offline leader's stale position as the reference, and does not mark it", () => {
    const copies = [copy("nats-0", { isLeader: true, offline: true, deliveredStreamSeq: 10 }), copy("nats-1", { deliveredStreamSeq: 5000 })];
    renderApp(<CopiesTable kind="consumer" copies={copies} />);
    const [leader, follower] = rows();
    expect(leader).not.toHaveClass("odd");
    expect(follower).not.toHaveClass("odd");
    expect(copiesDisagree("consumer", copies)).toBe(false);
  });

  it("takes the value most copies match as the reference, whatever the server order", () => {
    const h = 3_600_000;
    for (const order of [[0, 0.9 * h, 1.8 * h], [1.8 * h, 0.9 * h, 0]]) {
      expect(commonCreated(order.map((x) => t0 + x))).toBe(t0 + 0.9 * h);
      const copies = order.map((x, i) => copy(`nats-${i}`, { created: t0 + x, isLeader: i === 0 }));
      const { unmount } = renderApp(<CopiesTable kind="consumer" copies={copies} />);
      for (const r of rows()) expect(r).not.toHaveClass("odd");
      unmount();
    }
    for (const order of [[1000, 1090, 1180], [1180, 1090, 1000]]) {
      expect(commonPosition(order)).toBe(1090);
      const copies = order.map((v, i) => copy(`nats-${i}`, { deliveredStreamSeq: v, isLeader: i === 0 }));
      const { unmount } = renderApp(<CopiesTable kind="consumer" copies={copies} />);
      for (const r of rows()) expect(r).not.toHaveClass("odd");
      unmount();
    }
  });

  it("breaks a tie by the smallest value", () => {
    // 1000 and 1100 each match both; the smaller is the reference.
    expect(commonPosition([1100, 1000])).toBe(1000);
  });

  it("heads each row with its server", () => {
    renderApp(<CopiesTable kind="consumer" copies={[copy("nats-0", { isLeader: true }), copy("nats-1")]} />);
    expect(screen.getByRole("rowheader", { name: "nats-0" })).toBeInTheDocument();
  });
});

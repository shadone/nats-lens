import { act, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useState } from "react";
import { useNavigate } from "react-router";
import { renderApp } from "../test/render";
import { connectionFindings, useConnectionFindings } from "./connectionFindings";
import type { Finding } from "./model";
import * as api from "./requests";
vi.mock("./requests", () => ({ listFindings: vi.fn() }));
const f = (code: string, key: string) => ({ id: `${code}:${key}`, checkCode: code, entity: { kind: "client", key } }) as Finding;

describe("connection check identity", () => {
  it("matches service checks, full-name checks and unnamed checks using their own identities", () => {
    const rows = [f("client-001", "orders-service"), f("client-008", "orders-service"), f("client-009", "orders-service"), f("client-002", "orders-service@orders-service-a"), f("client-003", "orders-service@orders-service-a"), f("client-004", "orders-service@orders-service-a"), f("client-005", "orders-service@orders-service-a"), f("client-006", "orders-service"), f("client-007", "(unnamed)")];
    expect(connectionFindings(rows, "orders-service@orders-service-a")).toEqual(rows.slice(0, 7));
    expect(connectionFindings(rows, "orders-service@orders-service-b")).toEqual(rows.slice(0, 3));
    expect(connectionFindings(rows, "orders-service-extra@orders-service-extra-a")).toEqual([]);
    expect(connectionFindings(rows, "")).toEqual([rows[8]]);
    expect(connectionFindings(rows, "@orders-service-a")).toEqual([rows[8]]);
    expect(connectionFindings([{ ...rows[0], entity: { ...rows[0].entity!, kind: "server" } }], "orders-service")).toEqual([]);
  });
});
it("keys the batch by system, fixed range and newest collection, and waits for a real collection", async () => {
  let epoch: (s: string | undefined) => void = () => {};
  let go: ReturnType<typeof useNavigate> = () => {};
  const spec = { kind: "fixed", from: Date.parse("2026-10-17T13:30:00Z"), to: Date.parse("2026-10-17T14:30:00Z") } as const;
  vi.mocked(api.listFindings).mockResolvedValueOnce({ items: [f("client-001", "orders-service")], nextPageToken: "", totalCount: 1 }).mockImplementation(() => new Promise(() => {}));
  function Probe() {
    const [e, set] = useState<string>(); epoch = set; go = useNavigate();
    const q = useConnectionFindings(spec, e);
    return <span>{q.data?.items[0]?.id ?? "Unknown"}</span>;
  }
  renderApp(<Probe />, "/connections?system=shop-a");
  expect(api.listFindings).not.toHaveBeenCalled();
  act(() => epoch("7"));
  await screen.findByText("client-001:orders-service");
  expect(api.listFindings).toHaveBeenCalledWith("shop-a", spec, expect.objectContaining({ includeMuted: true }), expect.any(AbortSignal));
  act(() => epoch("8"));
  await waitFor(() => expect(api.listFindings).toHaveBeenCalledTimes(2));
  expect(screen.getByText("Unknown")).toBeInTheDocument();
  act(() => go("/connections?system=shop-b"));
  await waitFor(() => expect(api.listFindings).toHaveBeenCalledTimes(3));
  expect(screen.getByText("Unknown")).toBeInTheDocument();
});

import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp } from "../test/render";
import { OverviewTiles } from "./OverviewTiles";
import { TrendChart } from "./TrendChart";

it("links each Overview tile to its metric investigation with global context", () => {
  const ids = ["servers", "meta_leader", "streams", "kv", "object_stores", "consumers", "accounts", "clients", "storage"];
  const paths = ["/metrics/servers", "/metrics/meta-changes", "/metrics/streams", "/metrics/kv-buckets", "/metrics/object-stores", "/metrics/durable-consumers", "/metrics/accounts", "/metrics/connections", "/capacity/storage-change"];
  renderApp(<OverviewTiles tiles={ids.map(id => ({ id, label: id, description: "Details", unit: "count", valueSuffix: "", value2Suffix: "", subtitle: "", status: "ok" }))} windows={[]} start={0} end={1000} />, "/?range=6h&system=example&account=APP");
  ids.forEach((id, i) => {
    const a = screen.getByRole("link", { name: id });
    expect(new URL(a.getAttribute("href")!, "http://example.com").pathname).toBe(paths[i]);
    expect(a.getAttribute("href")).toContain("system=example");
    expect(a.getAttribute("href")).toContain("range=6h");
    expect(a.closest(".card")?.querySelector("a button, a svg")).toBeNull();
  });
  expect(screen.getByRole("link", { name: "clients" }).getAttribute("href")).toContain("account=APP");
});

describe("chart header navigation", () => {
  it("supports an encoded detail destination without nesting chart interactions", () => {
    renderApp(<TrendChart title="Traffic" to="/consumers/APP%2Fone/orders%3F/worker%23" start={0} end={1000} layers={[]} />, "/?range=6h&system=example&account=APP");
    expect(screen.getByRole("link", { name: "Traffic" })).toHaveAttribute("href", "/consumers/APP%2Fone/orders%3F/worker%23?range=6h&system=example&account=APP");
  });
});

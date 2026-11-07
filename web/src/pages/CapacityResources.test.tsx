import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { prod, stream } from "../test/fixtures/capacity";
import { renderApp } from "../test/render";
import { Capacity } from "./Capacity";

vi.mock("../api/requests", () => ({
  getCapacity: vi.fn(),
  getLensInfo: vi.fn(() => Promise.resolve({ collectIntervalMs: 30_000, live: true, demo: true })),
  getShellStatus: vi.fn(() => Promise.reject(new Error("no shell"))),
}));
const getCapacity = vi.mocked(requests.getCapacity);
beforeEach(() => {
  getCapacity.mockReset();
  const d = prod();
  d.streams = [stream("orders"), stream("KV_preferences", { kind: "kv" }), stream("OBJ_images", { kind: "object" })];
  d.streamsTotal = 3;
  getCapacity.mockResolvedValue(d);
});
const region = () => within(screen.getByRole("region", { name: "Resources by what bounds them" }));

it("names resource types and bucket identities, with scoped detail links", async () => {
  renderApp(<Capacity />, "/capacity?system=example-prod&account=APP&range=6h");
  await screen.findByRole("columnheader", { name: "Resource" });
  expect(region().getByText("Stream", { exact: true })).toBeInTheDocument();
  expect(region().getByText("KV bucket", { exact: true })).toBeInTheDocument();
  expect(region().getByText("Object store", { exact: true })).toBeInTheDocument();
  expect(region().getByText("Underlying stream: KV_preferences")).toBeInTheDocument();
  expect(region().getByText("Underlying stream: OBJ_images")).toBeInTheDocument();
  const link = region().getByRole("link", { name: "preferences" });
  const url = new URL(link.getAttribute("href")!, "http://example.com");
  expect(url.pathname).toBe("/kv-stores/APP/preferences");
  expect(url.searchParams.get("system")).toBe("example-prod");
  expect(url.searchParams.get("account")).toBe("APP");
  expect(url.searchParams.get("range")).toBe("6h");
  expect(region().getByRole("link", { name: "images" })).toHaveAttribute("href", expect.stringContaining("/object-stores/APP/images?"));
});

it("filters loaded resources in the URL without changing server totals or refetching", async () => {
  renderApp(<Capacity />, "/capacity?system=example-prod&range=6h&account=APP");
  const filter = await screen.findByRole("combobox", { name: "Resource type" });
  expect(filter).toHaveValue("all");
  await userEvent.selectOptions(filter, "kv");
  expect(region().getAllByRole("rowheader")).toHaveLength(1);
  expect(region().queryByRole("link", { name: "orders" })).toBeNull();
  expect(screen.getByText("1 of 3 loaded resources shown. Server cards and page totals include all resource types.")).toBeInTheDocument();
  const location = screen.getByTestId("location").textContent!;
  for (const param of ["resource=kv", "system=example-prod", "range=6h", "account=APP"]) expect(location).toContain(param);
  expect(screen.getByRole("region", { name: "Server nats-0" })).toBeInTheDocument();
  expect(getCapacity).toHaveBeenCalledTimes(1);
  await userEvent.selectOptions(filter, "object");
  expect(region().getByRole("link", { name: "images" })).toBeInTheDocument();
  await userEvent.selectOptions(filter, "stream");
  expect(region().getByRole("link", { name: "orders" })).toBeInTheDocument();
  await userEvent.selectOptions(filter, "all");
  expect(region().getAllByRole("rowheader")).toHaveLength(3);
  expect(screen.getByTestId("location").textContent).not.toContain("resource=");
});

it("honors a bookmarked filter and explains truncation without claiming whole-system type counts", async () => {
  const d = prod(); d.streamsTotal = 812; getCapacity.mockResolvedValue(d);
  renderApp(<Capacity />, "/capacity?resource=object");
  expect(await screen.findByText("No object stores among the loaded resources.")).toBeInTheDocument();
  expect(screen.getByText("The first 9 of 812 resources are loaded. Type filters apply only to these loaded resources.")).toBeInTheDocument();
  expect(screen.getByRole("combobox", { name: "Resource type" })).toHaveValue("object");
});

it("treats unknown URL filter values as All", async () => {
  renderApp(<Capacity />, "/capacity?resource=unknown");
  expect(await screen.findByRole("combobox", { name: "Resource type" })).toHaveValue("all");
  expect(region().getAllByRole("rowheader")).toHaveLength(3);
});

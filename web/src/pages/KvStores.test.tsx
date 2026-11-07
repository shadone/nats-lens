import { create } from "@bufbuild/protobuf";
import { durationFromMs } from "@bufbuild/protobuf/wkt";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { lens } from "../api/client";
import { KeyValueStoreSchema, ListKeyValueStoresResponseSchema } from "../gen/natslens/v1/buckets_pb";
import { EntityType, PlacementState, Status } from "../gen/natslens/v1/common_pb";
import { renderApp } from "../test/render";
import { KvStores } from "./KvStores";

vi.mock("../api/client", () => ({ lens: { listKeyValueStores: vi.fn() } }));
const list = vi.mocked(lens.listKeyValueStores);

const ref = (bucket: string) => ({ type: EntityType.STREAM, key: `APP/KV_${bucket}`, name: `KV_${bucket}`, account: "APP", stream: `KV_${bucket}` });

const present = create(KeyValueStoreSchema, {
  entity: ref("customer-config"),
  bucket: "customer-config",
  present: true,
  expected: true,
  history: 5,
  ttl: durationFromMs(0),
  maxBytes: BigInt(64 << 20),
  replicas: 3,
  storage: "file",
  keys: BigInt(41),
  bytes: BigInt(96 << 10),
  messages: BigInt(214),
  watchers: 35,
  status: Status.OK,
  bytesPerDay: 1024,
  placement: [
    { server: "nats-0", state: PlacementState.LEADER },
    { server: "nats-1", state: PlacementState.IN_SYNC },
    { server: "nats-2", state: PlacementState.BEHIND },
  ],
});
const small = create(KeyValueStoreSchema, {
  entity: ref("worker-presence"),
  bucket: "worker-presence",
  present: true,
  replicas: 1,
  keys: BigInt(18),
  bytes: BigInt(4 << 10),
  watchers: 6,
  status: Status.OK,
  ttl: durationFromMs(120_000),
  allowMsgTtl: true,
  placement: [{ server: "nats-1", state: PlacementState.LEADER }],
});
const missing = create(KeyValueStoreSchema, {
  entity: ref("label-templates"),
  bucket: "label-templates",
  present: false,
  expected: true,
  status: Status.WARNING,
  findingCodes: ["stream-011"],
});

function rowNames() {
  return screen
    .getAllByRole("row")
    .slice(1)
    .map((r) => within(r).getAllByRole("cell")[1].textContent ?? "");
}

describe("KV stores", () => {
  beforeEach(() => {
    list.mockReset();
    list.mockResolvedValue(create(ListKeyValueStoresResponseSchema, { keyValueStores: [missing, present, small] }));
  });

  it("links each present bucket to its resource page and shows a dash, never 0, for what was not observed", async () => {
    renderApp(<KvStores />, "/kv-stores?system=example-prod&range=6h");
    const link = await screen.findByRole("link", { name: "customer-config" });
    expect(link).toHaveAttribute("href", "/kv-stores/APP/customer-config?range=6h&system=example-prod");
    // At a narrow width the table scrolls in its card, with a shadow at the cut edge.
    expect(link.closest(".table-wrap")).toHaveClass("scroll-cue");
    expect(link.closest("tr")!.querySelector(".pl")!.closest("td")).toHaveClass("pl-cell");
    expect(screen.queryByRole("link", { name: "label-templates" })).toBeNull();
    const row = screen.getByText("label-templates").closest("tr")!;
    expect(within(row).getByText("missing")).toBeInTheDocument();
    expect(within(row).getByText("stream-011", { exact: false })).toBeInTheDocument();
    expect(within(row).queryAllByText(/^[0+]*0(\s|$)/)).toHaveLength(0);
    expect(within(row).getAllByText("not observed", { exact: false }).length).toBeGreaterThan(3);
    // The keys column says what it counts.
    expect(screen.getByRole("columnheader", { name: /keys/i })).toHaveAttribute("title", "subjects, including deleted keys");
    expect(screen.getByText(/subjects, including deleted keys/, { selector: "p, span, div" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: /watchers/i })).toHaveAttribute("title", "consumers on the bucket's stream");
    // A bucket without placement shows a dash in that cell, not an empty one.
    expect(within(row).getAllByRole("cell")[3]).toHaveTextContent("not observed");
  });

  it("ignores a sort parameter that names no column", async () => {
    for (const sort of ["__proto__", "constructor", "-toString", "hasOwnProperty"]) {
      const { unmount } = renderApp(<KvStores />, `/kv-stores?sort=${sort}`);
      expect(await screen.findByText("customer-config")).toBeInTheDocument();
      expect(rowNames()[0]).toContain("label-templates");
      unmount();
    }
  });

  it("sends the page's system and account scope", async () => {
    renderApp(<KvStores />, "/kv-stores?system=example-global&account=APP");
    await screen.findByText("customer-config");
    expect(list).toHaveBeenCalledWith(expect.objectContaining({ system: "example-global", account: "APP" }), expect.anything());
  });

  it("sorts by a column, leaving values that were not observed last", async () => {
    renderApp(<KvStores />, "/kv-stores");
    await screen.findByText("customer-config");
    expect(rowNames()[0]).toContain("label-templates");
    await userEvent.click(screen.getByRole("button", { name: /^Bytes/ }));
    await waitFor(() => expect(rowNames()[0]).toContain("customer-config"));
    expect(rowNames().at(-1)).toContain("label-templates");
    expect(screen.getByRole("columnheader", { name: /^Bytes/ })).toHaveAttribute("aria-sort", "descending");
    await userEvent.click(screen.getByRole("button", { name: /^Bytes/ }));
    await waitFor(() => expect(rowNames()[0]).toContain("worker-presence"));
    expect(rowNames().at(-1)).toContain("label-templates");
  });

  it("draws one square per replica and a legend that draws each state's mark", async () => {
    renderApp(<KvStores />, "/kv-stores");
    await screen.findByText("customer-config");
    const row = screen.getByText("customer-config").closest("tr")!;
    expect(within(row).getByRole("img", { name: /nats-0 leader, nats-1 in sync, nats-2 behind/ })).toBeInTheDocument();
    const legend = screen.getByRole("list", { name: "Placement legend" });
    for (const word of ["leader", "in sync", "behind"]) {
      const item = within(legend).getByText(word).closest("li")!;
      expect(item.querySelector("svg")).not.toBeNull();
    }
    expect(within(legend).queryByText("offline")).toBeNull();
  });
});

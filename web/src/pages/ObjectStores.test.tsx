import { create } from "@bufbuild/protobuf";
import { durationFromMs } from "@bufbuild/protobuf/wkt";
import { screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { lens } from "../api/client";
import { ListObjectStoresResponseSchema, ObjectStoreSchema } from "../gen/natslens/v1/buckets_pb";
import { EntityType, PlacementState, Status } from "../gen/natslens/v1/common_pb";
import { renderApp } from "../test/render";
import { ObjectStores } from "./ObjectStores";

vi.mock("../api/client", () => ({ lens: { listObjectStores: vi.fn() } }));

const ref = (bucket: string) => ({ type: EntityType.STREAM, key: `APP/OBJ_${bucket}`, name: `OBJ_${bucket}`, account: "APP", stream: `OBJ_${bucket}` });

describe("Object stores", () => {
  it("ignores a sort parameter that names no column", async () => {
    vi.mocked(lens.listObjectStores).mockResolvedValue(
      create(ListObjectStoresResponseSchema, { objectStores: [create(ObjectStoreSchema, { entity: ref("a"), bucket: "a", present: true, status: Status.OK })] }),
    );
    renderApp(<ObjectStores />, "/object-stores?sort=__proto__");
    expect(await screen.findByRole("link", { name: "a" })).toBeInTheDocument();
  });

  it("links a bucket to its stream, marks it sealed, and shows a dash for a missing one", async () => {
    vi.mocked(lens.listObjectStores).mockResolvedValue(
      create(ListObjectStoresResponseSchema, {
        objectStores: [
          create(ObjectStoreSchema, {
            entity: ref("image-output"), bucket: "image-output", present: true, expected: true, chunks: BigInt(5870), bytes: BigInt(3 << 30),
            maxBytes: BigInt(8) << BigInt(30), replicas: 3, storage: "file", ttl: durationFromMs(48 * 3600_000), sealed: true, status: Status.OK,
            placement: [{ server: "nats-0", state: PlacementState.LEADER }, { server: "nats-1", state: PlacementState.OFFLINE }],
          }),
          create(ObjectStoreSchema, { entity: ref("image-input"), bucket: "image-input", present: false, expected: true, status: Status.NO_DATA }),
        ],
      }),
    );
    renderApp(<ObjectStores />, "/object-stores?account=APP");
    const link = await screen.findByRole("link", { name: "image-output" });
    expect(link.getAttribute("href")).toMatch(/^\/streams\/APP\/OBJ_image-output/);
    const row = link.closest("tr")!;
    expect(within(row).getByText("sealed", { exact: false })).toBeInTheDocument();
    expect(within(row).getByText("2d")).toBeInTheDocument();
    expect(within(row).getByText("of 8 GiB", { exact: false })).toBeInTheDocument();
    const gone = screen.getByText("image-input").closest("tr")!;
    expect(within(gone).queryAllByText(/^[0+]*0(\s|$)/)).toHaveLength(0);
    expect(within(gone).getByText("missing")).toBeInTheDocument();
    expect(vi.mocked(lens.listObjectStores)).toHaveBeenCalledWith(expect.objectContaining({ account: "APP" }), expect.anything());
    expect(within(screen.getByRole("list", { name: "Placement legend" })).getByText("offline")).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: /^Messages/ })).toHaveAttribute("title", "chunks plus one metadata message per object");
    expect(screen.queryByRole("columnheader", { name: /chunks/i })).toBeNull();
    expect(within(row).getByText("5,870")).toBeInTheDocument();
  });
});

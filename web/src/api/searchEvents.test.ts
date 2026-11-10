import { create } from "@bufbuild/protobuf";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { lens } from "./client";
import { searchEvents } from "./events";
import { EventSchema } from "../gen/natslens/v1/events_pb";

vi.mock("./client", () => ({ lens: { listEvents: vi.fn() } }));

beforeEach(() => vi.clearAllMocks());

describe("searchEvents", () => {
  it("asks for one page of events worth reading, repeats folded, filtered by text", async () => {
    vi.mocked(lens.listEvents).mockResolvedValue({ events: [create(EventSchema, { id: "7", repeats: 4 })], nextPageToken: "n", totalCount: 9 } as never);
    const r = await searchEvents("sys-a", { kind: "fixed", from: 0, to: 3_600_000 }, "analytics", 5);
    expect(vi.mocked(lens.listEvents).mock.calls[0][0]).toMatchObject({ system: "sys-a", text: "analytics", hideRoutine: true, foldRepeats: true, pageSize: 5 });
    expect(r).toMatchObject({ nextPageToken: "n", totalCount: 9, items: [{ id: "7", repeats: 4 }] });
  });
});

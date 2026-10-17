// The system list and the badge's live summaries share one ListSystems
// query over the default live range.
import { create } from "@bufbuild/protobuf";
import { screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { Status } from "../gen/natslens/v1/common_pb";
import { ListSystemsResponseSchema } from "../gen/natslens/v1/systems_pb";
import { renderApp } from "../test/render";
import { lens } from "./client";
import { useLiveSystemSummaries, useSystems } from "./systems";

vi.mock("./client", () => ({ lens: { listSystems: vi.fn() } }));
const l = vi.mocked(lens);

function Probe() {
  const systems = useSystems().data;
  const summaries = useLiveSystemSummaries().data;
  return (
    <div data-testid="probe">
      {systems?.defaultSystem ?? "-"} {systems?.systems.map((s) => s.name).join(",") ?? "-"} {summaries?.map((s) => s.status).join(",") ?? "-"}
    </div>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  l.listSystems.mockResolvedValue(
    create(ListSystemsResponseSchema, {
      systems: [{ system: { id: "shop-prod", name: "Production" }, status: Status.WARNING }, { system: { id: "shop-staging", name: "" }, status: Status.OK }],
      defaultSystem: "shop-prod",
    }) as never,
  );
});

it("asks ListSystems once for the system list and the live summaries", async () => {
  renderApp(<Probe />);
  await waitFor(() => expect(screen.getByTestId("probe")).toHaveTextContent("shop-prod Production,shop-staging warning,ok"));
  expect(l.listSystems).toHaveBeenCalledTimes(1);
  expect(l.listSystems).toHaveBeenCalledWith(expect.objectContaining({ stripBuckets: 48 }), expect.anything());
});

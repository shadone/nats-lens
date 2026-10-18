// The system list, the badges and the header's status share the shell's
// one status call of the page's system.
import { create } from "@bufbuild/protobuf";
import { act, screen, waitFor } from "@testing-library/react";
import { useNavigate } from "react-router";
import { beforeEach, expect, it, vi } from "vitest";
import { Status } from "../gen/natslens/v1/common_pb";
import { CollectionState, GetShellStatusResponseSchema } from "../gen/natslens/v1/systems_pb";
import { renderApp } from "../test/render";
import { lens } from "./client";
import { useShellStatus } from "./queries";
import { useCurrentSystem, useSystemBadges, useSystems } from "./systems";

vi.mock("./client", () => ({ lens: { getShellStatus: vi.fn(), listSystems: vi.fn() } }));
const l = vi.mocked(lens);

let navigate: (to: string) => void = () => {};
function Probe() {
  navigate = useNavigate();
  const systems = useSystems().data;
  const badges = useSystemBadges().data;
  const known = useCurrentSystem().known;
  const tally = useShellStatus().data?.tally;
  return (
    <div data-testid="probe">
      {systems?.defaultSystem ?? "-"} {systems?.systems.map((s) => s.name).join(",") ?? "-"} {badges?.map((s) => `${s.status}${s.collecting ? "" : " stopped"}`).join(",") ?? "-"}{" "}
      known={String(known)} warnings={tally?.warning ?? "-"}
    </div>
  );
}

/** Set to hold the staging system's answer until release is called. */
let holdStaging = false;
let release: (() => void) | undefined;

beforeEach(() => {
  vi.clearAllMocks();
  holdStaging = false;
  release = undefined;
  l.getShellStatus.mockImplementation((async (req: { system?: string }) => {
    if (holdStaging && req.system === "shop-staging") await new Promise<void>((r) => (release = r));
    const known = !req.system || req.system === "shop-prod" || req.system === "shop-staging";
    return create(GetShellStatusResponseSchema, {
      tally: known ? { warning: 2 } : undefined,
      systems: [
        { system: { id: "shop-prod", name: "Production" }, status: Status.WARNING, collection: CollectionState.COLLECTING, collecting: true },
        { system: { id: "shop-staging", name: "" }, status: Status.OK, collection: CollectionState.NOT_CONNECTED },
      ],
      defaultSystem: "shop-prod",
      systemKnown: known,
    });
  }) as never);
});

it("asks the shell's status once for the system list, the badges and the header", async () => {
  renderApp(<Probe />);
  await waitFor(() => expect(screen.getByTestId("probe")).toHaveTextContent("shop-prod Production,shop-staging warning,ok stopped known=true warnings=2"));
  expect(l.getShellStatus).toHaveBeenCalledTimes(1);
  expect(l.listSystems).not.toHaveBeenCalled();
});

it("says a ?system= that names no configured system is unknown", async () => {
  renderApp(<Probe />, "/?system=nope");
  await waitFor(() => expect(screen.getByTestId("probe")).toHaveTextContent("known=false warnings=0"));
  expect(l.getShellStatus).toHaveBeenCalledTimes(1);
});

it("keeps the system list and the badges while the next system's status loads, but not the header's", async () => {
  holdStaging = true;
  renderApp(<Probe />, "/?system=shop-prod");
  await waitFor(() => expect(screen.getByTestId("probe")).toHaveTextContent("warnings=2"));
  act(() => navigate("/?system=shop-staging"));
  await waitFor(() => expect(release).toBeDefined());
  expect(screen.getByTestId("probe")).toHaveTextContent("shop-prod Production,shop-staging warning,ok stopped known=true warnings=-");
  await act(async () => release!());
  await waitFor(() => expect(screen.getByTestId("probe")).toHaveTextContent("warnings=2"));
});

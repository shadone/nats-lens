import { Code, ConnectError } from "@connectrpc/connect";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useNavigate } from "react-router";
import type { Replay as ReplayData } from "../api/adapt-replay";
import type { Timeline } from "../api/model";
import * as requests from "../api/requests";
import { rgone, rreplay, rstep } from "../test/fixtures/replay";
import { renderApp } from "../test/render";
import { Replay } from "./Replay";

vi.mock("../api/requests", () => ({
  getReplay: vi.fn(),
  getTimeline: vi.fn(),
  getLensInfo: vi.fn(() => new Promise(() => {})),
  getShellStatus: vi.fn(() => new Promise(() => {})),
}));

const T = (s: string) => Date.parse(s);
const FIXED = "from=2026-10-17T06:00:00Z&to=2026-10-17T12:00:00Z";
const AB = "a=2026-10-17T09:14:00Z&b=2026-10-17T09:50:00Z";

function timeline(): Timeline {
  return {
    start: T("2026-10-17T06:00:00Z"),
    bucketMs: 300_000,
    bucketCount: 72,
    rows: [],
    kubernetes: [],
    nats: [],
    windows: [{ start: T("2026-10-17T09:14:00Z"), end: T("2026-10-17T09:50:00Z"), label: "GKE node upgrade", detail: "" }],
    natsTruncated: false,
    findingTruncated: false,
    kubernetesTruncated: false,
  };
}

/** The demo's story, by system: example-prod's names a restart of nats-1, example-global's of nats-us-1. */
function story(sys: string, a: number, b: number): ReplayData {
  const who = sys === "example-global" ? "nats-us-1" : "nats-1";
  return rreplay(
    {
      a,
      b,
      steps: [
        rstep(`restart:${who}:${a}`, "09:18:00", { kind: "restart", title: `${who} restarts at ${new Date(a).toISOString().slice(11, 16)}` }),
        rstep("resolved:1", "10:05:00", { kind: "resolved", title: "order-jobs has quorum again", outsideRange: true }),
      ],
      counts: { serversRestarted: 1, streamsGoneWithoutDelete: 0, consumersGoneWithoutDelete: 1 },
      gone: [rgone({ notAnsweringAtB: ["nats-2"] })],
    },
    sys,
  );
}

let clip: ReturnType<typeof vi.fn>;

beforeEach(() => {
  clip = vi.fn(() => Promise.resolve());
  vi.mocked(requests.getTimeline).mockResolvedValue(timeline());
  vi.mocked(requests.getReplay).mockImplementation(async (sys, a, b) => story(sys, a, b));
});

afterEach(() => {
  vi.mocked(requests.getReplay).mockReset();
  vi.mocked(requests.getTimeline).mockReset();
});

function location() {
  return decodeURIComponent(screen.getByTestId("location").textContent ?? "");
}

function GoTo({ to }: { to: string }) {
  const navigate = useNavigate();
  return (
    <button type="button" onClick={() => navigate(to)}>
      go
    </button>
  );
}

describe("Replay", () => {
  it("writes the newest Kubernetes window of the range into a link without A and B, without a history entry", async () => {
    renderApp(<Replay />, `/replay?system=example-prod&${FIXED}`);
    await waitFor(() => expect(location()).toContain("a=2026-10-17T09:09:00Z&b=2026-10-17T09:55:00Z"));
    expect(screen.getByTestId("navigation")).toHaveTextContent("REPLACE");
    expect(await screen.findByText("nats-1 restarts at 09:09")).toBeInTheDocument();
    expect(requests.getReplay).toHaveBeenCalledWith("example-prod", T("2026-10-17T09:09:00Z"), T("2026-10-17T09:55:00Z"), expect.anything());
    expect(screen.getByText("Pick two moments; nats-lens tells you what changed in between, and in what order.")).toBeInTheDocument();
  });

  it("asks for the strip with every check, OK ones included", async () => {
    renderApp(<Replay />, `/replay?system=example-prod&${FIXED}&${AB}`);
    await screen.findByText("nats-1 restarts at 09:14");
    expect(requests.getTimeline).toHaveBeenCalledWith("example-prod", expect.objectContaining({ kind: "fixed" }), undefined, expect.anything(), true);
  });

  it("tells the story: steps, After B, counts and gone assets", async () => {
    renderApp(<Replay />, `/replay?system=example-prod&${FIXED}&${AB}`);
    await screen.findByText("nats-1 restarts at 09:14");
    expect(screen.getByRole("heading", { name: "After B" })).toBeInTheDocument();
    const counts = screen.getByRole("list", { name: "Between A and B" });
    expect(within(counts).getByText("Servers restarted").closest("li")).toHaveTextContent("1");
    expect(within(counts).getByText("Servers stopped").closest("li")).toHaveTextContent("not observed");
    const row = screen.getByRole("link", { name: "restock-alerts" }).closest("tr")!;
    expect(row).toHaveTextContent("3 servers: nats-0, nats-1, nats-2");
    expect(row).toHaveTextContent("none answering: nats-2");
    expect(row).toHaveTextContent("none captured");
    expect(screen.getByRole("link", { name: "Compare in Placement" }).getAttribute("href")).toContain("/placement?from=2026-10-17T08%3A50%3A00Z&to=2026-10-17T09%3A50%3A00Z&cmp=2026-10-17T09%3A14%3A00Z");
  });

  it("shows a moved handle's A–B as loading and the steps on screen as the earlier choice", async () => {
    renderApp(<Replay />, `/replay?system=example-prod&${FIXED}&${AB}`);
    await screen.findByText("nats-1 restarts at 09:14");
    vi.mocked(requests.getReplay).mockImplementation(() => new Promise(() => {}));
    fireEvent.keyDown(screen.getByRole("slider", { name: "Moment A" }), { key: "ArrowRight" });
    fireEvent.keyDown(screen.getByRole("slider", { name: "Moment A" }), { key: "ArrowRight" });
    expect(location()).toContain("a=2026-10-17T09:24:00Z");
    const status = await screen.findByText(/^Loading 17 Oct 09:24–17 Oct 09:50 UTC; below is the earlier choice, 17 Oct 09:14–17 Oct 09:50 UTC\.$/);
    expect(status.closest("[role=status]")).toBeInTheDocument();
    const old = screen.getByText("nats-1 restarts at 09:14").closest(".rp-story")!;
    expect(old).toHaveAttribute("aria-busy", "true");
    expect(old).toHaveClass("stale");
    expect(screen.getByRole("button", { name: "Export as incident log" })).toBeDisabled();
    // One call for the new A once the handle rests, not one per key.
    await waitFor(() => expect(requests.getReplay).toHaveBeenCalledWith("example-prod", T("2026-10-17T09:24:00Z"), T("2026-10-17T09:50:00Z"), expect.anything()));
    expect(vi.mocked(requests.getReplay).mock.calls.map((c) => c[1])).toEqual([T("2026-10-17T09:14:00Z"), T("2026-10-17T09:24:00Z")]);
  });

  it("shows nothing of the previous system while another system's replay loads", async () => {
    renderApp(
      <>
        <GoTo to={`/replay?system=example-global&${FIXED}&${AB}`} />
        <Replay />
      </>,
      `/replay?system=example-prod&${FIXED}&${AB}`,
    );
    await screen.findByText("nats-1 restarts at 09:14");
    vi.mocked(requests.getReplay).mockImplementation(() => new Promise(() => {}));
    await userEvent.click(screen.getByRole("button", { name: "go" }));
    expect(await screen.findByText("Loading the replay…")).toBeInTheDocument();
    expect(screen.queryByText("nats-1 restarts at 09:14")).toBeNull();
    expect(screen.queryByText("Servers restarted")).toBeNull();
  });

  it("copies an absolute link to A–B over a fixed range, and fixes the page's range too", async () => {
    const user = userEvent.setup();
    Object.defineProperty(navigator, "clipboard", { value: { writeText: clip }, configurable: true });
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(T("2026-10-17T12:00:00Z"));
    try {
      renderApp(<Replay />, `/replay?system=example-prod&range=6h&${AB}`);
      await screen.findByText("nats-1 restarts at 09:14");
      await user.click(screen.getByRole("button", { name: "Copy link to A–B" }));
      expect(clip).toHaveBeenCalledWith(
        `${window.location.origin}/replay?system=example-prod&a=2026-10-17T09%3A14%3A00Z&b=2026-10-17T09%3A50%3A00Z&from=2026-10-17T06%3A00%3A00Z&to=2026-10-17T12%3A00%3A00Z`,
      );
      expect(await screen.findByText(/^Link to A–B copied/)).toHaveAttribute("role", "status");
      expect(location()).toContain("from=2026-10-17T06:00:00Z&to=2026-10-17T12:00:00Z");
      expect(location()).not.toContain("range=");
    } finally {
      vi.useRealTimers();
    }
  });

  it("exports the story as a text file named by system and moments", async () => {
    let saved: Blob | undefined;
    const create = vi.fn((b: Blob) => {
      saved = b;
      return "blob:replay";
    });
    Object.assign(URL, { createObjectURL: create, revokeObjectURL: vi.fn() });
    const names: string[] = [];
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) {
      names.push(this.download);
    });
    renderApp(<Replay />, `/replay?system=example-prod&${FIXED}&${AB}`);
    await screen.findByText("nats-1 restarts at 09:14");
    await userEvent.click(screen.getByRole("button", { name: "Export as incident log" }));
    expect(names).toEqual(["replay-example-prod-20261017T091400Z-20261017T095000Z.txt"]);
    const text = await saved!.text();
    expect(text.startsWith("nats-lens replay\nSystem: example-prod\nA: 2026-10-17 09:14:00 UTC\nB: 2026-10-17 09:50:00 UTC\n")).toBe(true);
    expect(text).toContain("\nAfter B\n");
    expect(text).toContain(`Link: ${window.location.origin}/replay?system=example-prod`);
    click.mockRestore();
  });

  it("shows the server's refusal of an A older than the retention", async () => {
    vi.mocked(requests.getReplay).mockRejectedValue(new ConnectError("A is older than retention (14d); Replay reads the database only", Code.InvalidArgument));
    renderApp(<Replay />, `/replay?system=example-prod&${FIXED}&${AB}`);
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Could not load the replay.");
    expect(alert).toHaveTextContent("A is older than retention (14d); Replay reads the database only");
  });

  it("says when there was no collection at A", async () => {
    vi.mocked(requests.getReplay).mockImplementation(async (sys, a, b) => ({ ...story(sys, a, b), epochAAt: undefined, epochA: "0" }));
    renderApp(<Replay />, `/replay?system=example-prod&${FIXED}&${AB}`);
    expect(await screen.findByText(/^No collection at A: none started in the three collect intervals before it/)).toBeInTheDocument();
  });

  it("says why it changed a link's A and B, and asks for what GetReplay accepts", async () => {
    renderApp(<Replay />, `/replay?system=example-prod&${FIXED}&a=2026-10-17T09:50:00Z&b=2026-10-17T09:14:00Z`);
    expect(await screen.findByText("A was after B, so the two were swapped.")).toBeInTheDocument();
    await screen.findByText("nats-1 restarts at 09:14");
    expect(requests.getReplay).toHaveBeenCalledWith("example-prod", T("2026-10-17T09:14:00Z"), T("2026-10-17T09:50:00Z"), expect.anything());
  });
});

import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Overview as OverviewModel } from "../api/model";
import * as requests from "../api/requests";
import { renderApp } from "../test/render";
import { Overview } from "./Overview";

vi.mock("../api/requests", () => ({
  getOverview: vi.fn(),
  listFindings: vi.fn(),
  getTimeline: vi.fn(),
  listEvents: vi.fn(),
  listStreams: vi.fn(),
  muteFinding: vi.fn(),
  unmuteFinding: vi.fn(),
  getLensInfo: vi.fn(() => new Promise(() => {})),
}));

const END = Date.parse("2026-10-17T14:30:00Z");
const quiet: OverviewModel = {
  cluster: "example-prod",
  range: { start: END - 86_400_000, end: END },
  servers: [],
  tally: { critical: 0, warning: 0, info: 0, muted: 0, flapping: 0, resolved24h: 0 },
  tiles: [],
  windows: [],
};

function mockEmpty(o: OverviewModel) {
  vi.mocked(requests.getOverview).mockResolvedValue(o);
  vi.mocked(requests.listFindings).mockResolvedValue({ items: [], nextPageToken: "", totalCount: 0 });
  vi.mocked(requests.getTimeline).mockResolvedValue({ start: END - 86_400_000, bucketMs: 900_000, bucketCount: 96, rows: [], kubernetes: [], nats: [], windows: [], natsTruncated: false, findingTruncated: false, kubernetesTruncated: false });
  vi.mocked(requests.listEvents).mockResolvedValue({ items: [], nextPageToken: "", totalCount: 0 });
  vi.mocked(requests.listStreams).mockResolvedValue({ streams: [], totals: { bytes: 0, durableConsumers: 0, ephemeralConsumers: 0 } });
}

describe("Overview without findings", () => {
  it("shows an all-clear card and an empty state in every section", async () => {
    mockEmpty(quiet);
    renderApp(<Overview />);
    expect(await screen.findByText("Nothing needs attention")).toBeInTheDocument();
    expect(screen.getByLabelText("Findings by severity")).toBeInTheDocument();
    expect(screen.queryByLabelText("Other findings")).toBeNull();
    expect(screen.getByText(/No epoch stored in this range yet/)).toBeInTheDocument();
    expect(await screen.findByText(/No check has reported in this range/)).toBeInTheDocument();
    expect(await screen.findByText("No events in this range.")).toBeInTheDocument();
    expect(await screen.findByText(/No streams in the newest epoch/)).toBeInTheDocument();
    expect(screen.getAllByText(/No server reported in this range/).length).toBeGreaterThan(0);
  });

  it("shows the error with a retry when the overview fails", async () => {
    vi.mocked(requests.getOverview).mockRejectedValue(new Error("unavailable"));
    renderApp(<Overview />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Could not load the overview. unavailable");
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });
});

describe("Overview timeline lanes", () => {
  it("says when the lanes left out finding transitions", async () => {
    mockEmpty(quiet);
    vi.mocked(requests.getTimeline).mockResolvedValue({
      start: END - 86_400_000, bucketMs: 900_000, bucketCount: 96, rows: [], kubernetes: [], nats: [], windows: [],
      natsTruncated: false, findingTruncated: true, kubernetesTruncated: false,
    });
    renderApp(<Overview />);
    expect(await screen.findByText(/more finding transitions than the lanes show/)).toBeInTheDocument();
    expect(screen.queryByText(/more NATS events than the lanes show/)).toBeNull();
  });

  it("says when the Kubernetes lane was cut", async () => {
    mockEmpty(quiet);
    vi.mocked(requests.getTimeline).mockResolvedValue({
      start: END - 86_400_000, bucketMs: 900_000, bucketCount: 96, rows: [], kubernetes: [], nats: [], windows: [],
      natsTruncated: false, findingTruncated: false, kubernetesTruncated: true,
    });
    renderApp(<Overview />);
    expect(await screen.findByText(/more Kubernetes events than the lane shows/)).toBeInTheDocument();
    expect(screen.queryByText(/more finding transitions than the lanes show/)).toBeNull();
  });
});

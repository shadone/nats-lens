import { screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Entity, Finding, Overview as OverviewModel } from "../api/model";
import * as requests from "../api/requests";
import { renderApp } from "../test/render";
import { Overview } from "./Overview";

vi.mock("../api/requests", async (orig) => ({
  ...(await orig<typeof import("../api/requests")>()),
  getOverview: vi.fn(),
  listFindings: vi.fn(),
  getTimeline: vi.fn(),
  muteFinding: vi.fn(),
  unmuteFinding: vi.fn(),
  getLensInfo: vi.fn(() => new Promise(() => {})),
}));

vi.mock("../api/client", () => ({
  lens: {
    getTimeSeries: vi.fn(async () => ({ series: undefined })),
    listConsumers: vi.fn(async () => ({ consumers: [] })),
    listStreams: vi.fn(async () => ({ streams: [], totals: undefined })),
  },
}));

const END = Date.parse("2026-10-17T14:30:00Z");
const quiet: OverviewModel = {
  cluster: "example-prod",
  range: { start: END - 86_400_000, end: END },
  freshness: { epoch: "9", epochAt: Date.now() - 5_000, ageMs: 5_000, partial: false, live: true, intervalMs: 30_000, collecting: true },
  servers: [],
  tally: { critical: 0, warning: 0, info: 0, muted: 0, flapping: 0, resolved24h: 0 },
  tiles: [],
  windows: [],
};

const emptyTimeline = { start: END - 86_400_000, bucketMs: 900_000, bucketCount: 96, rows: [], kubernetes: [], nats: [], windows: [], natsTruncated: false, findingTruncated: false, kubernetesTruncated: false };

function consumerEnt(name: string): Entity {
  return { kind: "consumer", key: `APP/order-jobs/${name}`, name, account: "APP", stream: "order-jobs", consumer: name, server: "" };
}

function finding(id: string, e: Entity, extra: Partial<Finding> = {}): Finding {
  return {
    id,
    checkCode: "consumer-009",
    checkName: "Consumer creation time differs across servers",
    severity: "info",
    state: "firing",
    entity: e,
    detail: `Detail of ${e.name}.`,
    remediation: "",
    relatedConfig: [],
    relatedConfigSource: "",
    transitionsLastHour: 0,
    ...extra,
  };
}

function mock(o: OverviewModel, findings: Finding[] = []) {
  vi.mocked(requests.getOverview).mockResolvedValue(o);
  vi.mocked(requests.listFindings).mockResolvedValue({ items: findings, nextPageToken: "", totalCount: findings.length });
  vi.mocked(requests.getTimeline).mockResolvedValue(emptyTimeline);
}

beforeEach(() => vi.clearAllMocks());

describe("Overview without findings", () => {
  it("says serving normally, shows no cause card and an empty state in every section", async () => {
    mock(quiet);
    renderApp(<Overview />);
    expect(await screen.findByRole("status", { name: "Status" })).toHaveTextContent(/^Serving normally/);
    expect(screen.queryByRole("heading", { name: "Serving normally" })).toBeNull();
    expect(screen.queryByRole("heading", { level: 1 })).toBeNull();
    expect(screen.queryByRole("region", { name: "Findings" })).toBeNull();
    expect(screen.getByText(/No collection stored in this range yet/)).toBeInTheDocument();
    expect(await screen.findByText(/No consumer in the newest collection/)).toBeInTheDocument();
    expect(await screen.findByText(/No streams in the newest collection/)).toBeInTheDocument();
    expect(await screen.findByRole("img", { name: /^Findings: ok throughout/ })).toBeInTheDocument();
  });

  it("shows the error with a retry when the overview fails", async () => {
    vi.mocked(requests.getOverview).mockRejectedValue(new Error("unavailable"));
    renderApp(<Overview />);
    expect(await screen.findByRole("alert")).toHaveTextContent("Could not load the overview. unavailable");
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });
});

describe("Overview cause cards", () => {
  it("shows four consumer-009 findings on order-jobs as one card of four", async () => {
    const fs = ["resize-worker", "thumb-worker", "job-cache-projector", "audit-writer"].map((n) => finding(`c9:${n}`, consumerEnt(n)));
    mock({ ...quiet, tally: { ...quiet.tally, info: 4 } }, fs);
    renderApp(<Overview />);
    const card = await screen.findByRole("article", { name: "Consumer creation time differs across servers: 4 findings" });
    expect(within(card).getByText(/consumer-009\s*× 4/)).toBeInTheDocument();
    expect(within(card).getByText("order-jobs")).toBeInTheDocument();
    expect(within(card).getAllByRole("listitem")).toHaveLength(4);
    expect(within(card).getByText("Detail of thumb-worker.")).toBeInTheDocument();
    expect(within(card).getByRole("button", { name: "Mute consumer-009 on audit-writer" })).toBeInTheDocument();
    expect(screen.getAllByRole("article")).toHaveLength(1);
    expect(screen.getByRole("status", { name: "Status" })).toHaveTextContent(/^Serving normally/);
    expect(screen.getByText("4 INFO")).toBeInTheDocument();
    // The cards' h3s sit under an h2, hidden but in the outline.
    const h2 = screen.getByRole("heading", { level: 2, name: "Findings" });
    expect(h2).toHaveClass("sr-only");
    expect(h2.compareDocumentPosition(within(card).getByRole("heading", { level: 3 })) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("lists muted findings with their reason in quotes, apart from the cards", async () => {
    const m = finding("m", consumerEnt("resize-worker"), {
      mute: { id: "mu", checkCode: "consumer-009", reason: "rebuilt on purpose", mutedBy: "ops", unmutedBy: "" },
    });
    mock({ ...quiet, tally: { ...quiet.tally, muted: 1 } }, [m]);
    renderApp(<Overview />);
    const strip = await screen.findByRole("region", { name: "Muted findings" });
    expect(strip).toHaveTextContent("“rebuilt on purpose” ops");
    expect(screen.queryByRole("article")).toBeNull();
  });
});

describe("Overview timeline lanes", () => {
  it("says when the lanes left out finding transitions", async () => {
    mock(quiet);
    vi.mocked(requests.getTimeline).mockResolvedValue({ ...emptyTimeline, findingTruncated: true });
    renderApp(<Overview />);
    expect(await screen.findByText(/more finding transitions than the lanes show/)).toBeInTheDocument();
    expect(screen.queryByText(/more NATS events than the lanes show/)).toBeNull();
  });

  it("says when the Kubernetes lane was cut", async () => {
    mock(quiet);
    vi.mocked(requests.getTimeline).mockResolvedValue({ ...emptyTimeline, kubernetesTruncated: true });
    renderApp(<Overview />);
    expect(await screen.findByText(/more Kubernetes events than the lane shows/)).toBeInTheDocument();
    expect(screen.queryByText(/more finding transitions than the lanes show/)).toBeNull();
  });
});

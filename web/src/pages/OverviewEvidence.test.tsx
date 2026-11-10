import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { lens } from "../api/client";
import type { Entity, Finding, Overview as OverviewModel } from "../api/model";
import * as requests from "../api/requests";
import { PlacementState } from "../gen/natslens/v1/common_pb";
import { renderApp } from "../test/render";
import { Overview } from "./Overview";

vi.mock("../api/requests", async (orig) => ({
  ...(await orig<typeof import("../api/requests")>()),
  getOverview: vi.fn(),
  listFindings: vi.fn(),
  getTimeline: vi.fn(),
  muteFinding: vi.fn(),
  getLensInfo: vi.fn(() => new Promise(() => {})),
}));

vi.mock("../api/client", () => ({
  lens: {
    getTimeSeries: vi.fn(async () => ({ series: undefined })),
    getTimeSeriesBatch: vi.fn(async (req: { queries: unknown[] }) => ({ results: req.queries.map(() => ({ error: "" })) })),
    listConsumers: vi.fn(async () => ({ consumers: [] })),
    listStreams: vi.fn(async () => ({ streams: [], totals: undefined })),
    listAssetPlacement: vi.fn(),
  },
}));

const END = Date.parse("2026-10-17T14:30:00Z");
const quiet: OverviewModel = {
  cluster: "example-prod",
  range: { start: END - 86_400_000, end: END },
  freshness: { epoch: "9", epochAt: Date.now() - 5_000, ageMs: 5_000, partial: false, live: true, intervalMs: 30_000, collecting: true },
  servers: [],
  tally: { critical: 0, warning: 4, info: 0, muted: 0, flapping: 0, resolved24h: 0 },
  tiles: [],
  windows: [],
};
const emptyTimeline = { start: END - 86_400_000, bucketMs: 900_000, bucketCount: 96, rows: [], kubernetes: [], nats: [], windows: [], natsTruncated: false, findingTruncated: false, kubernetesTruncated: false };

const consumerEnt = (name: string): Entity => ({ kind: "consumer", key: `APP/order-jobs/${name}`, name, account: "APP", stream: "order-jobs", consumer: name, server: "" });
const serverEnt = (name: string): Entity => ({ kind: "server", key: name, name, account: "", stream: "", consumer: "", server: name });

function finding(id: string, e: Entity, extra: Partial<Finding> = {}): Finding {
  return { id, checkCode: "consumer-009", checkName: "Consumer creation time differs across servers", severity: "warning", state: "firing", entity: e, detail: `Detail of ${e.name}.`, remediation: "", relatedConfig: [], relatedConfigSource: "", transitionsLastHour: 0, ...extra };
}

const workers = ["resize-worker", "thumb-worker", "job-cache-projector", "audit-writer"];

function mock(findings: Finding[], otherAccountCount = 0) {
  vi.mocked(requests.getOverview).mockResolvedValue(quiet);
  vi.mocked(requests.listFindings).mockResolvedValue({ items: findings, nextPageToken: "", totalCount: findings.length, otherAccountCount });
  vi.mocked(requests.getTimeline).mockResolvedValue(emptyTimeline);
  // Every consumer led by nats-eu-3, with one follower on nats-eu-1.
  vi.mocked(lens.listAssetPlacement).mockImplementation((async (req: { entities: { key: string }[] }) => ({
    assets: req.entities.map((e) => ({
      entity: { key: e.key },
      reported: true,
      leader: "nats-eu-3",
      replicas: [
        { server: "nats-eu-3", state: PlacementState.LEADER },
        { server: "nats-eu-1", state: PlacementState.IN_SYNC },
      ],
    })),
  })) as unknown as typeof lens.listAssetPlacement);
}

beforeEach(() => vi.clearAllMocks());

describe("Overview cause evidence", () => {
  it("shows that nats-eu-3 leads all four consumers, the table by server, the nats CLI check and acceptance", async () => {
    mock(workers.map((n) => finding(`c9:${n}`, consumerEnt(n))));
    renderApp(<Overview />);
    const card = await screen.findByRole("article", { name: "Consumer creation time differs across servers: 4 findings" });
    expect(await within(card).findByText("nats-eu-3 leads 4 of 4 consumers.")).toBeInTheDocument();
    // One read for the card's four consumers.
    expect(lens.listAssetPlacement).toHaveBeenCalledTimes(1);
    expect(vi.mocked(lens.listAssetPlacement).mock.calls[0][0].entities).toHaveLength(4);
    fireEvent.click(within(card).getByText("Evidence"));
    const table = within(card).getByRole("table", { name: "Affected consumers by server" });
    const rows = within(table).getAllByRole("row");
    expect(rows[1]).toHaveTextContent("nats-eu-34 of 4resize-worker leader, thumb-worker leader, job-cache-projector leader, audit-writer leader");
    expect(rows[2]).toHaveTextContent(/^nats-eu-14 of 4/);
    expect(within(card).getByText("nats consumer report -- order-jobs")).toBeInTheDocument();
    expect(within(card).getByRole("button", { name: "Copy nats CLI check" })).toBeInTheDocument();
    expect(within(card).getByText("Run with a nats context for account APP on the default system.")).toBeInTheDocument();
    // consumer-009 reports a fault, not a configuration choice.
    expect(within(card).queryByRole("button", { name: /^Accept/ })).toBeNull();
  });

  it("offers acceptance for a check of a configuration choice, without the growth review it lacks", async () => {
    mock([finding("c16", consumerEnt("resize-worker"), { severity: "info", checkCode: "consumer-016", checkName: "Single replica on a replicated stream", acceptable: true })]);
    renderApp(<Overview />);
    const card = await screen.findByRole("article", { name: "Single replica on a replicated stream: 1 finding" });
    fireEvent.click(within(card).getByRole("button", { name: "Accept consumer-016 on resize-worker as by design" }));
    const dialog = await screen.findByRole("dialog", { name: "Accept as by design" });
    expect(dialog).toHaveTextContent("Accepts consumer-016 Single replica on a replicated stream for resize-worker");
    expect(dialog).toHaveTextContent("The finding leaves the Overview until the review date.");
    expect(dialog).not.toHaveTextContent("growth over a week");
  });

  it("says when a card's assets were beyond the 200 one read asks for", async () => {
    // 26 cards of 8 consumers each: 208 assets listed, 200 asked for.
    const fs = Array.from({ length: 26 }, (_, c) => Array.from({ length: 8 }, (_, i) => finding(`f${c}-${i}`, consumerEnt(`w${c}-${i}`), { checkCode: `consumer-${100 + c}`, checkName: `Check ${c}` }))).flat();
    mock(fs);
    renderApp(<Overview />);
    const last = await screen.findByRole("article", { name: "Check 25: 8 findings" });
    await waitFor(() => expect(lens.listAssetPlacement).toHaveBeenCalled());
    expect(vi.mocked(lens.listAssetPlacement).mock.calls[0][0].entities).toHaveLength(200);
    fireEvent.click(within(last).getByText("Evidence"));
    expect(await within(last).findByText("Not read: more than 200 assets.")).toBeInTheDocument();
    const first = screen.getByRole("article", { name: "Check 0: 8 findings" });
    fireEvent.click(within(first).getByText("Evidence"));
    expect(within(first).queryByText("Not read: more than 200 assets.")).toBeNull();
  });

  it("offers no acceptance for a fault, even one it would allow by severity", async () => {
    mock([finding("crit", consumerEnt("resize-worker"), { severity: "critical", checkCode: "consumer-001", checkName: "Consumer quorum lost" })]);
    renderApp(<Overview />);
    const card = await screen.findByRole("article", { name: "Consumer quorum lost: 1 finding" });
    expect(within(card).getByRole("button", { name: "Mute consumer-001 on resize-worker" })).toBeInTheDocument();
    expect(within(card).queryByRole("button", { name: /^Accept/ })).toBeNull();
    fireEvent.click(within(card).getByText("Evidence"));
    expect(within(card).getByText("nats consumer info --json -- order-jobs resize-worker")).toBeInTheDocument();
  });
});

describe("Overview findings by account", () => {
  it("asks for the scoped account's findings, labels the ones across accounts and counts the others", async () => {
    mock([finding("srv", serverEnt("nats-eu-2"), { checkCode: "server-001", checkName: "Server not reporting" })], 3);
    renderApp(<Overview />, "/?account=SUPPLIERS");
    const card = await screen.findByRole("article", { name: "Server not reporting: 1 finding" });
    expect(within(card).getByText("Across accounts")).toBeInTheDocument();
    expect(vi.mocked(requests.listFindings).mock.calls[0][2]).toMatchObject({ account: "SUPPLIERS", includeMuted: true });
    expect(screen.getByText(/3 findings of other accounts are not shown\./)).toBeInTheDocument();
    expect(screen.getByText("The summary, trends and timeline cover all accounts; the findings, busiest consumers and streams follow the account.")).toBeInTheDocument();
    // A server finding is evidence of itself, without a placement read.
    expect(lens.listAssetPlacement).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Show all accounts" }));
    await waitFor(() => expect(vi.mocked(requests.listFindings).mock.calls.at(-1)?.[2]).toMatchObject({ account: "" }));
    await waitFor(() => expect(screen.queryByText("Across accounts")).toBeNull());
  });

  it("keeps the account on the card's link to the rest of its findings", async () => {
    mock(Array.from({ length: 9 }, (_, i) => finding(`c${i}`, consumerEnt(`w${i}`))));
    renderApp(<Overview />, "/?account=APP");
    const more = await screen.findByRole("link", { name: "1 more" });
    expect(more.getAttribute("href")).toBe("/findings?check=consumer-009&account=APP");
  });

  it("says one finding in the singular and nothing unscoped", async () => {
    mock([finding("srv", serverEnt("nats-eu-2"), { checkCode: "server-001", checkName: "Server not reporting" })], 1);
    renderApp(<Overview />, "/?account=APP");
    expect(await screen.findByText(/1 finding of another account is not shown\./)).toBeInTheDocument();
  });
});

import { screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderApp } from "../test/render";
import { A, B, rstep, server } from "../test/fixtures/replay";
import { ReplayCounts } from "./ReplayCounts";
import { ReplaySteps } from "./ReplaySteps";

vi.mock("../api/requests", () => ({ getLensInfo: vi.fn(() => new Promise(() => {})), getShellStatus: vi.fn(() => new Promise(() => {})) }));

const M = { a: A, b: B };

const steps = [
  rstep("restart:1", "09:18:00", {
    kind: "restart",
    end: Date.parse("2026-10-17T09:18:30Z"),
    title: "nats-1 restarts",
    body: "Back at 09:18:30.",
    evidence: [
      { kind: "event", id: "77", label: "pod nats-1 Killing" },
      { kind: "collection", id: "120", time: Date.parse("2026-10-17T09:18:30Z"), label: "collection of 09:18:30", entity: server("nats-1") },
      { kind: "collection", id: "121", time: Date.parse("2026-10-17T09:19:00Z"), label: "collection of 09:19:00" },
      { kind: "finding", id: "stream-001:APP/orders", label: "stream-001 (firing)" },
    ],
    evidenceCount: 6,
  }),
  rstep("gap:1", "09:30:00", { kind: "gap", end: Date.parse("2026-10-17T09:33:00Z"), title: "No collections for 3 min" }),
  rstep("quorum:1", "09:40:00", { kind: "quorum", severity: "critical", openAtB: true, title: "order-jobs has no quorum" }),
  rstep("resolved:1", "10:05:00", { kind: "resolved", title: "order-jobs has quorum again", outsideRange: true }),
];

describe("ReplaySteps", () => {
  it("lists the steps in order with their times and evidence links", () => {
    renderApp(<ReplaySteps steps={steps} moments={M} notes={[]} />);
    const list = screen.getByRole("list", { name: "What happened, in order" });
    const items = within(list).getAllByRole("listitem").filter((li) => li.classList.contains("rp-step"));
    expect(items.map((li) => li.querySelector(".rp-when")?.textContent)).toEqual(["09:18:00–09:18:30", "09:30:00–09:33:00", "09:40:00–still at B"]);
    expect(items[1]).toHaveClass("gap");
    expect(within(items[0]).getByRole("link", { name: "pod nats-1 Killing" })).toHaveAttribute("href", "/events?id=77");
    expect(within(items[0]).getByRole("link", { name: "collection of 09:18:30" })).toHaveAttribute("href", "/servers/nats-1?from=2026-10-17T08%3A18%3A30Z&to=2026-10-17T09%3A18%3A31Z");
    expect(within(items[0]).getByRole("link", { name: "collection of 09:19:00" })).toHaveAttribute("href", "/placement?from=2026-10-17T08%3A19%3A00Z&to=2026-10-17T09%3A19%3A01Z");
    expect(within(items[0]).getByRole("link", { name: "stream-001 (firing)" })).toHaveAttribute("href", "/findings/stream-001%3AAPP%2Forders");
    expect(items[0]).toHaveTextContent("and 2 more");
    expect(items[2].querySelector(".rp-dot")).toHaveClass("critical");
  });

  it("writes a span as two times, and the kind and severity once for a screen reader", () => {
    const k8s = rstep("k8s:1", "09:16:00", { kind: "kubernetes", title: "Node drained" });
    const { container } = renderApp(<ReplaySteps steps={[...steps, k8s]} moments={M} notes={[]} />);
    const span = container.querySelector('[data-step="restart:1"] .rp-when')!;
    expect([...span.querySelectorAll("time")].map((t) => t.getAttribute("datetime"))).toEqual(["2026-10-17T09:18:00Z", "2026-10-17T09:18:30Z"]);
    expect(container.querySelector('[data-step="k8s:1"] .sr-only')).toHaveTextContent(/^Kubernetes:$/);
    expect(container.querySelector('[data-step="resolved:1"] .sr-only')).toHaveTextContent(/^Resolved:$/);
    expect(container.querySelector('[data-step="quorum:1"] .sr-only')).toHaveTextContent(/^Quorum, critical:$/);
  });

  it("folds a step's leader elections into one line per new leader that opens to the list", () => {
    const el = (id: string, stream: string, leader: string) => ({ kind: "event" as const, id, label: `${stream} -> ${leader} (term 4${id})` });
    const st = rstep("restart:9", "09:20:00", {
      kind: "restart",
      title: "nats-1 restarts",
      evidence: [{ kind: "event", id: "1", label: "pod nats-1 Killing" }, el("2", "order-jobs", "nats-0"), el("3", "order-archive", "nats-0"), el("4", "media-windows", "nats-2"), el("5", "price-changes", "nats-0")],
      evidenceCount: 25,
    });
    const { container } = renderApp(<ReplaySteps steps={[st]} moments={M} notes={[]} />);
    const ev = within(container.querySelector('[data-step="restart:9"] .rp-ev') as HTMLElement);
    const lines = [...(container.querySelector('[data-step="restart:9"] .rp-ev') as HTMLElement).children].map((li) => li.querySelector("summary")?.textContent ?? li.textContent);
    expect(lines).toEqual(["event pod nats-1 Killing", "events 3 leader elections on nats-0", "event media-windows -> nats-2 (term 44)", "and 20 more"]);
    expect(ev.getByRole("link", { name: "order-archive -> nats-0 (term 43)" })).toHaveAttribute("href", "/events?id=3");
  });

  it("puts the steps after B under an After B divider, each tagged", () => {
    renderApp(<ReplaySteps steps={steps} moments={M} notes={[]} />);
    const after = screen.getByRole("list", { name: "After B" });
    expect(within(after).getByText("order-jobs has quorum again")).toBeInTheDocument();
    expect(within(after).getByText("outside the range")).toBeInTheDocument();
    const divider = screen.getByRole("heading", { name: "After B" });
    expect(divider.compareDocumentPosition(after) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(within(screen.getByRole("list", { name: "What happened, in order" })).queryByText("outside the range")).toBeNull();
    expect(screen.getByText("Built from collections, NATS advisories and Kubernetes events; every line links to its evidence.")).toBeInTheDocument();
  });

  it("gives a span that began on an earlier day its day, in a wider column", () => {
    const gap = rstep("gap:old", "09:00:00", { kind: "gap", time: Date.parse("2026-10-14T23:00:00Z"), end: Date.parse("2026-10-17T09:10:00Z"), title: "nats-lens was not running" });
    const { container } = renderApp(<ReplaySteps steps={[gap]} moments={M} notes={[]} />);
    expect(container.querySelector('[data-step="gap:old"] .rp-when')).toHaveTextContent("14 Oct 23:00:00–09:10:00");
    expect(container.querySelector(".rp-list")).toHaveClass("days");
  });

  it("shows what may be missing", () => {
    renderApp(<ReplaySteps steps={steps} moments={M} notes={["Steps after 09:45 may be missing: the read limit was reached."]} />);
    expect(screen.getByRole("note")).toHaveTextContent("Steps after 09:45 may be missing: the read limit was reached.");
  });

  it("says when nothing was observed", () => {
    renderApp(<ReplaySteps steps={[]} moments={M} notes={[]} />);
    expect(screen.getByText("Nothing that nats-lens observed changed between A and B.")).toBeInTheDocument();
  });
});

describe("ReplayCounts", () => {
  const at = (h: string) => Date.parse(`2026-10-17T${h}Z`);

  it("shows observed counts as tiles, zero quieter", () => {
    renderApp(<ReplayCounts counts={{ serversRestarted: 3, serversStopped: 0, leadersMoved: 2, leaderElections: 5 }} />);
    const tiles = within(screen.getByRole("list", { name: "Between A and B" })).getAllByRole("listitem");
    expect(tiles.map((t) => t.textContent)).toEqual(["3Servers restarted", "0Servers stoppednot back by B", "2Leaders movedof 5 elections"]);
    expect(tiles[0]).toHaveClass("t-warning");
    expect(tiles[1]).toHaveClass("zero");
  });

  it("names a count without a reason the response gives in a plain line", () => {
    renderApp(<ReplayCounts counts={{ serversRestarted: 1 }} unobserved={[{ count: "findingsRaised" }]} />);
    const line = screen.getByText("Not observed:").closest("p")!;
    expect(line).toHaveTextContent(/^Not observed: servers stopped, .* and findings raised\.$/);
  });

  it("names a lost events connection as the reason for the event counts it left unset, from the response and not from the gap steps", () => {
    // A lost events connection that left only the event counts unset.
    const events = ["leadersMoved", "metaLeaderChanges", "quorumLostReports", "clientReconnects"];
    const unobserved = events.map((count) => ({ count, reason: "events_connection_lost" as const, from: at("09:20:00"), to: at("09:22:00") }));
    const counts = { serversRestarted: 1, serversStopped: 0, streamsGoneWithoutDelete: 0, consumersGoneWithoutDelete: 0, assetsGoneWithDelete: 0, assetsUnknownAtB: 0, nodePoolsUpgraded: 0, nodesDrained: 0, findingsRaised: 2 };
    renderApp(<ReplayCounts counts={counts} unobserved={unobserved} />);
    expect(screen.getByText("Not observed (events were not captured, as an events connection was lost 09:20–09:22):").closest("p")).toHaveTextContent(
      /^Not observed \(events were not captured, as an events connection was lost 09:20–09:22\): leaders moved, meta leader changes, quorum lost advisories and client reconnects\.$/,
    );
    expect(screen.queryByText("Not observed:")).toBeNull();
  });

  it("groups the counts by reason and by the times of each gap", () => {
    const unobserved = [
      { count: "serversRestarted", reason: "no_collections" as const },
      { count: "serversStopped", reason: "no_collection_at_b" as const },
      { count: "streamsGoneWithoutDelete", reason: "gone_not_read" as const },
      { count: "consumersGoneWithoutDelete", reason: "gone_not_read" as const },
      { count: "assetsGoneWithDelete", reason: "no_collection_at_a" as const },
      { count: "assetsUnknownAtB", reason: "events_not_captured" as const },
      { count: "leadersMoved", reason: "collector_gap" as const, from: at("09:40:00") },
      { count: "metaLeaderChanges", reason: "collector_gap" as const, from: at("09:40:00") },
      { count: "quorumLostReports", reason: "read_limit" as const, from: at("09:45:00") },
      { count: "nodePoolsUpgraded", reason: "kubernetes_gap" as const, from: at("09:05:00"), to: at("09:10:00") },
      { count: "nodesDrained", reason: "kubernetes_not_watched" as const },
      { count: "clientReconnects", reason: "collector_gap" as const, from: at("09:00:00"), to: at("09:05:00") },
      { count: "findingsRaised", reason: "node_unnamed" as const },
    ];
    renderApp(<ReplayCounts counts={{}} unobserved={unobserved} />);
    const lines = [...document.querySelectorAll(".rp-unseen")].map((p) => p.textContent);
    expect(lines).toEqual([
      "Not observed (no collection started between A and B): servers restarted.",
      "Not observed (no collection at B): servers stopped.",
      "Not observed (the gone assets were not read): streams gone without a delete and consumers gone without a delete.",
      "Not observed (no collection at A): assets gone with a delete.",
      "Not observed (no event was stored up to B): assets unknown at B.",
      "Not observed (nats-lens was not running 09:40–still at B): leaders moved and meta leader changes.",
      "Not observed (the read limit was reached at 09:45): quorum lost advisories.",
      "Not observed (Kubernetes was not watched 09:05–09:10): node pools upgraded.",
      "Not observed (Kubernetes is not watched for this system): nodes drained.",
      "Not observed (nats-lens was not running 09:00–09:05): client reconnects.",
      "Not observed (a node event names no node): findings raised.",
    ]);
  });

  it("names the pool label reason for the pools when the nodes are counted", () => {
    renderApp(<ReplayCounts counts={{ nodesUpgraded: 4 }} unobserved={[{ count: "nodePoolsUpgraded", reason: "no_pool_labels" }]} />);
    expect(screen.getByText("Nodes upgraded")).toBeInTheDocument();
    expect(screen.queryByText(/node pools upgraded/)).toBeNull();
  });

  it("says nothing of elections when there were none", () => {
    renderApp(<ReplayCounts counts={{ leadersMoved: 0, leaderElections: 0 }} />);
    expect(screen.getByText("Leaders moved").closest("li")).toHaveTextContent(/^0Leaders moved$/);
  });
});

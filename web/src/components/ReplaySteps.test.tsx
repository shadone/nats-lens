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
  it("shows observed counts as tiles, zero quieter, and names the unobserved ones in one line", () => {
    renderApp(<ReplayCounts counts={{ serversRestarted: 3, serversStopped: 0, leadersMoved: 2, leaderElections: 5 }} kubernetesWatched />);
    const tiles = within(screen.getByRole("list", { name: "Between A and B" })).getAllByRole("listitem");
    expect(tiles.map((t) => t.textContent)).toEqual(["3Servers restarted", "0Servers stoppednot back by B", "2Leaders movedof 5 elections"]);
    expect(tiles[0]).toHaveClass("t-warning");
    expect(tiles[1]).toHaveClass("zero");
    const line = screen.getByText("Not observed:").closest("p")!;
    expect(line).toHaveTextContent(/^Not observed: streams gone without a delete, .*node pools upgraded, nodes drained, client reconnects, findings raised\.$/);
    expect(screen.queryByText(/Kubernetes is not watched/)).toBeNull();
  });

  it("says once that Kubernetes is not watched instead of listing its counts as not observed", () => {
    renderApp(<ReplayCounts counts={{ serversRestarted: 1 }} kubernetesWatched={false} />);
    expect(screen.getByText("Kubernetes is not watched for this system, so node pools upgraded and nodes drained are not counted.")).toBeInTheDocument();
    expect(screen.getByText("Not observed:").closest("p")).not.toHaveTextContent("nodes drained");
  });

  it("gives a gap between A and B as the possible reason for unset node counts", () => {
    renderApp(<ReplayCounts counts={{ serversRestarted: 1 }} kubernetesWatched gap />);
    expect(screen.getByText("There is a gap between A and B (see the gap steps), which can leave node pools upgraded and nodes drained not counted.")).toBeInTheDocument();
  });

  it("says nothing of elections when there were none", () => {
    renderApp(<ReplayCounts counts={{ leadersMoved: 0, leaderElections: 0 }} kubernetesWatched />);
    expect(screen.getByText("Leaders moved").closest("li")).toHaveTextContent(/^0Leaders moved$/);
  });

  it("names the nodes upgraded when no pool label counts the pools", () => {
    renderApp(<ReplayCounts counts={{ nodesUpgraded: 4 }} kubernetesWatched />);
    expect(screen.getByText("Nodes upgraded")).toBeInTheDocument();
    expect(screen.queryByText("Node pools upgraded")).toBeNull();
  });
});

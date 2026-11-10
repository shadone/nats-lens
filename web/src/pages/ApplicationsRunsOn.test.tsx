import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { app, dispatcher, prod, resizer } from "../test/fixtures/applications";
import { renderApp } from "../test/render";
import { Applications } from "./Applications";

vi.mock("../api/requests", () => ({
  getApplications: vi.fn(),
  getLensInfo: vi.fn(() => Promise.resolve({ identityMode: "request", version: "dev", cluster: "c", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: true })),
  getShellStatus: vi.fn(() => Promise.reject(new Error("no shell in this test"))),
}));
vi.mock("../api/systems", async (orig) => ({ ...(await orig<typeof import("../api/systems")>()), useAccounts: vi.fn(() => ({ data: undefined })) }));
const getApps = vi.mocked(requests.getApplications);

afterEach(() => {
  getApps.mockReset();
});

function table() {
  return within(screen.getByRole("table"));
}

function runsOnCell(name: string) {
  const tr = table().getByRole("rowheader", { name: new RegExp(`^${name}`) }).closest("tr") as HTMLElement;
  return within(tr).getAllByRole("cell")[1];
}

/** example-global's kind of page: client pods watched, the watch in payments failing. */
function watched() {
  return prod({
    clientPodsWatched: true,
    clientPodsUnwatched: [{ source: "client pods in payments", since: Date.parse("2026-10-17T14:05:00Z"), error: "Kubernetes API returned HTTP 403" }],
    applications: [
      dispatcher({ connections: 5, runsOn: { matched: 3, ambiguous: 0, workloads: [{ namespace: "shop", kind: "StatefulSet", name: "callback-dispatcher", pods: 3, connections: 3 }], workloadsTotal: 1, nodes: ["node-eu-1", "node-eu-2"], nodesTotal: 2, pods: 3 } }),
      app({ runsOn: { matched: 0, ambiguous: 0, workloads: [], workloadsTotal: 0, nodes: [], nodesTotal: 0, pods: 0 } }),
      resizer({
        runsOn: {
          matched: 4,
          ambiguous: 0,
          workloads: [
            { namespace: "media", kind: "Deployment", name: "resize-worker", pods: 3, connections: 3 },
            { namespace: "media", kind: "CronJob", name: "resize-backfill", pods: 1, connections: 1 },
          ],
          workloadsTotal: 2,
          nodes: ["node-eu-1", "node-eu-2"],
          nodesTotal: 2,
          pods: 4,
        },
      }),
    ],
  });
}

describe("Applications Runs on", () => {
  it("adds nothing while client pods are not watched", async () => {
    getApps.mockResolvedValue(prod());
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    expect(screen.queryByText(/Runs on/)).toBeNull();
    expect(screen.queryByText(/Client pods not watched/)).toBeNull();
  });

  it("shows where each application runs after Pods, and what matched no pod", async () => {
    getApps.mockResolvedValue(watched());
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    expect(table().getAllByRole("columnheader").map((h) => h.querySelector(".sort-btn")?.textContent).slice(0, 3)).toEqual(["Application", "Pods", "Runs on"]);
    // Two workloads both show; the nodes open behind the pods and nodes.
    expect(runsOnCell("resize-worker")).toHaveTextContent(/^Deployment media\/resize-workerCronJob media\/resize-backfill4 pods on 2 nodesnode-eu-1node-eu-2$/);
    const nodes = within(runsOnCell("resize-worker")).getByText("4 pods on 2 nodes").closest("details") as HTMLDetailsElement;
    expect(nodes.open).toBe(false);
    await userEvent.click(within(runsOnCell("resize-worker")).getByText("4 pods on 2 nodes"));
    expect(nodes.open).toBe(true);
    expect(runsOnCell("callback-dispatcher")).toHaveTextContent("StatefulSet shop/callback-dispatcher3 pods on 2 nodesnode-eu-1node-eu-23 of 5 connections matched to pods");
    expect(runsOnCell("order-service")).toHaveTextContent(/^No connection matched to a pod$/);
  });

  it("says when the watch saw more client pods than it keeps", async () => {
    getApps.mockResolvedValue({ ...watched(), clientPodsCapped: true });
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    expect(screen.getByText(/^nats-lens keeps at most/).closest("p")).toHaveTextContent(
      "Too many client pods. nats-lens keeps at most 50,000 client pods and saw more since it started: connections from the others match no pod.",
    );
  });

  it("names the client pod watch that was failing, and says in the footnote what matches", async () => {
    getApps.mockResolvedValue(watched());
    renderApp(<Applications />, "/applications");
    await screen.findByRole("table");
    expect(screen.getByText(/^nats-lens could not watch/).closest("p")).toHaveTextContent(
      "Client pods not watched. nats-lens could not watch the client pods in payments since 14:05 UTC (Kubernetes API returned HTTP 403): Runs on can miss the pods there.",
    );
    expect(screen.getByText(/^Runs on matches each connection's client address/)).toHaveTextContent(/Workloads are named from the pod's owner name, not read from the cluster\.$/);
    expect(screen.queryByText(/Too many client pods/)).toBeNull();
  });
});

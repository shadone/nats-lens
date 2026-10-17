import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Connection } from "../api/model";
import * as requests from "../api/requests";
import { renderApp } from "../test/render";
import { Connections } from "./Connections";

vi.mock("../api/requests", () => ({ listConnections: vi.fn(), listServers: vi.fn(), getLensInfo: vi.fn(() => new Promise(() => {})) }));

const conn = (cid: string, name: string, tooling: boolean): Connection => ({
  key: `nats-0/${cid}`, server: "nats-0", cid, name, lang: "go", version: "1", account: "ACJQ1234", accountName: "APP",
  tooling, user: "", ip: "127.0.0.1",
});

describe("Connections", () => {
  it("tags a tooling connection for every reader and counts the hidden ones", async () => {
    vi.mocked(requests.listServers).mockResolvedValue({ servers: [], expected: [] } as never);
    vi.mocked(requests.listConnections).mockResolvedValue({
      items: [conn("1", "nats-cli", true), conn("2", "worker", false)], nextPageToken: "", totalCount: 2, epoch: "3", toolingHidden: 4,
    });
    renderApp(<Connections />, "/connections");
    expect(await screen.findByText("4 tooling connections hidden", { exact: false })).toBeInTheDocument();
    const tag = screen.getByText("tooling");
    expect(tag.textContent).toContain("nats-lens's own connection");
    expect(screen.getAllByText(/tooling/).length).toBeGreaterThan(0);
    expect(screen.getAllByText("APP").length).toBe(2);
  });
});

describe("Connections name filter", () => {
  it("includes the tooling connections a name filter matches and says so", async () => {
    vi.mocked(requests.listServers).mockResolvedValue({ servers: [], expected: [] } as never);
    vi.mocked(requests.listConnections).mockResolvedValue({
      items: [conn("1", "natslens-events", true)], nextPageToken: "", totalCount: 1, epoch: "3", toolingHidden: 0,
    });
    renderApp(<Connections />, "/connections?name=natslens");
    expect(await screen.findByText(/shown because the name filter matches them/)).toBeInTheDocument();
    expect(vi.mocked(requests.listConnections).mock.calls.at(-1)?.[2]).toMatchObject({ nameContains: "natslens", includeTooling: true });
  });
});

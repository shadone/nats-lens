import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useNavigate } from "react-router";
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
    vi.mocked(requests.listConnections).mockResolvedValue({
      items: [conn("1", "nats-cli", true), conn("2", "worker", false)], nextPageToken: "", totalCount: 2, epoch: "3", toolingHidden: 4, servers: [],
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
    vi.mocked(requests.listConnections).mockResolvedValue({
      items: [conn("1", "natslens-events", true)], nextPageToken: "", totalCount: 1, epoch: "3", toolingHidden: 0, servers: [],
    });
    renderApp(<Connections />, "/connections?name=natslens");
    expect(await screen.findByText(/shown because the name filter matches them/)).toBeInTheDocument();
    expect(vi.mocked(requests.listConnections).mock.calls.at(-1)?.[2]).toMatchObject({ nameContains: "natslens", includeTooling: true });
  });
});

describe("Connections pages", () => {
  it("never sends one system's page token to another", async () => {
    let go: ReturnType<typeof useNavigate> = () => {};
    function Nav() {
      go = useNavigate();
      return null;
    }
    vi.mocked(requests.listConnections).mockImplementation(async (sys, _spec, f) => ({
      items: [conn("1", "worker", false)], nextPageToken: sys === "sys-a" && !f.pageToken ? "a-token-2" : "", totalCount: 200, epoch: "3", toolingHidden: 0, servers: [],
    }));
    renderApp(
      <>
        <Connections />
        <Nav />
      </>,
      "/connections?system=sys-a",
    );
    await userEvent.click(await screen.findByRole("button", { name: "Next" }));
    await waitFor(() => expect(requests.listConnections).toHaveBeenCalledWith("sys-a", expect.anything(), expect.objectContaining({ pageToken: "a-token-2" }), expect.anything()));
    vi.mocked(requests.listConnections).mockClear();
    act(() => go("/connections?system=sys-b"));
    await waitFor(() => expect(requests.listConnections).toHaveBeenCalledWith("sys-b", expect.anything(), expect.anything(), expect.anything()));
    for (const c of vi.mocked(requests.listConnections).mock.calls) expect(c[2].pageToken ?? "").toBe("");
  });
});

describe("Connections server filter", () => {
  it("offers the servers ListConnections names, all of them while one is chosen, without asking ListServers", async () => {
    vi.mocked(requests.listServers).mockClear();
    vi.mocked(requests.listConnections).mockResolvedValue({
      items: [conn("1", "worker", false)], nextPageToken: "", totalCount: 1, epoch: "3", toolingHidden: 0, servers: ["nats-0", "nats-1", "nats-2"],
    });
    renderApp(<Connections />, "/connections?server=nats-1");
    const select = screen.getByRole("combobox", { name: "Server" });
    await waitFor(() => expect(within(select).getAllByRole("option").map((o) => o.textContent)).toEqual(["All servers", "nats-0", "nats-1", "nats-2"]));
    expect(select).toHaveValue("nats-1");
    expect(vi.mocked(requests.listConnections).mock.calls.at(-1)?.[2]).toMatchObject({ server: "nats-1" });
    expect(requests.listServers).not.toHaveBeenCalled();
  });
});

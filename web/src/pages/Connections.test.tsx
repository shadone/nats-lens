import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useNavigate } from "react-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Connection, Finding } from "../api/model";
import * as requests from "../api/requests";
import { renderApp } from "../test/render";
import { Connections } from "./Connections";

vi.mock("../api/requests", () => ({ listConnections: vi.fn(), listFindings: vi.fn(), listServers: vi.fn(), getLensInfo: vi.fn(() => new Promise(() => {})) }));

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

const finding = (code: string, key: string, state: Finding["state"] = "firing"): Finding => ({
  id: `${code}:${key}`, checkCode: code, checkName: "Client check", severity: "warning", state,
  entity: { kind: "client", key, name: key, account: "", stream: "", consumer: "", server: "" },
  detail: "", remediation: "", relatedConfig: [], relatedConfigSource: "", transitionsLastHour: 0,
});
beforeEach(() => {
  vi.mocked(requests.listFindings).mockReset().mockResolvedValue({ items: [], nextPageToken: "", totalCount: 0 });
});

describe("Connections finding discovery", () => {
  const names = ["orders-service@orders-service-a", "orders-service@orders-service-b", "orders-service-extra@orders-service-extra-a"];
  beforeEach(() => {
    vi.mocked(requests.listConnections).mockResolvedValue({
      items: names.map((name, i) => ({ ...conn(String(i), name, false), rttMs: 180 })),
      nextPageToken: "", totalCount: 3, epoch: "7", toolingHidden: 0, servers: [],
    });
  });
  it("links service RTT findings from every pod but full-name findings only from that name, in one bounded call", async () => {
    vi.mocked(requests.listFindings).mockResolvedValue({ items: [finding("client-001", "orders-service"), finding("client-002", names[0])], nextPageToken: "", totalCount: 2 });
    renderApp(<Connections />, "/connections?system=shop&range=6h&account=APP");
    const links = await screen.findAllByRole("link", { name: /client-001.*Firing/ });
    expect(links).toHaveLength(2);
    expect(await screen.findAllByRole("link", { name: /client-002.*Firing/ })).toHaveLength(1);
    const url = new URL(links[0].getAttribute("href")!, "http://example.com");
    expect(url.pathname).toBe("/findings/client-001%3Aorders-service");
    expect(url.searchParams.get("system")).toBe("shop");
    expect(url.searchParams.get("range")).toBe("6h");
    expect(url.searchParams.has("account")).toBe(false);
    expect(screen.getByText("No matching finding")).toBeInTheDocument();
    expect(requests.listFindings).toHaveBeenCalledTimes(1);
    expect(requests.listFindings).toHaveBeenCalledWith("shop", expect.anything(), expect.objectContaining({ includeMuted: true, pageSize: 500, states: ["firing", "changed", "flapping", "resolved"] }), expect.anything());
    links[0].focus();
    await userEvent.keyboard("{Enter}");
    expect(screen.getByTestId("location").textContent).toContain("/findings/client-001%3Aorders-service");
  });
  it("keeps the finding batch and row event links while paging forward and back", async () => {
    vi.mocked(requests.listConnections).mockImplementation(async (_sys, _spec, filter) => ({
      items: [conn(filter.pageToken ? "2" : "1", filter.pageToken ? names[1] : names[0], false)],
      nextPageToken: filter.pageToken ? "" : "page-2", totalCount: 101, epoch: "7", toolingHidden: 0, servers: [],
    }));
    vi.mocked(requests.listFindings).mockResolvedValue({ items: [finding("client-001", "orders-service")], nextPageToken: "", totalCount: 1 });
    renderApp(<Connections />, "/connections?system=shop&range=6h");
    await screen.findByRole("link", { name: /client-001.*Firing/ });
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(await screen.findByRole("link", { name: names[1] })).toHaveAttribute("href", expect.stringContaining("/events?client=" + encodeURIComponent(names[1])));
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "Previous" }));
    expect(await screen.findByRole("link", { name: names[0] })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Previous" })).toBeDisabled();
    expect(requests.listFindings).toHaveBeenCalledTimes(1);
  });
  it("labels resolved and muted findings without reporting them as firing", async () => {
    const muted = { ...finding("client-001", "orders-service", "changed"), mute: { id: "m" } } as Finding;
    vi.mocked(requests.listFindings).mockResolvedValue({ items: [muted, finding("client-002", names[0], "resolved")], nextPageToken: "", totalCount: 2 });
    renderApp(<Connections />);
    expect(await screen.findAllByRole("link", { name: /client-001.*Changed.*Muted/ })).toHaveLength(2);
    expect(await screen.findByRole("link", { name: /client-002.*Resolved/ })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /Firing/ })).not.toBeInTheDocument();
  });
  it.each(["loading", "error", "truncated"])("keeps missing finding state unknown when %s", async state => {
    if (state === "loading") vi.mocked(requests.listFindings).mockImplementation(() => new Promise(() => {}));
    if (state === "error") vi.mocked(requests.listFindings).mockRejectedValue(new Error("unavailable"));
    if (state === "truncated") vi.mocked(requests.listFindings).mockResolvedValue({ items: [], nextPageToken: "more", totalCount: 501 });
    renderApp(<Connections />);
    await screen.findByRole("link", { name: names[0] });
    await waitFor(() => expect(screen.getAllByText(/Unknown/)).toHaveLength(3));
    expect(screen.queryByText("No matching finding")).not.toBeInTheDocument();
    expect(screen.getByText(/persistence.*collections/i)).toBeInTheDocument();
  });
  it("does not carry findings across systems, ranges or newest collections", async () => {
    let go: ReturnType<typeof useNavigate> = () => {};
    function Nav() { go = useNavigate(); return null; }
    vi.mocked(requests.listFindings).mockResolvedValueOnce({ items: [finding("client-001", "orders-service")], nextPageToken: "", totalCount: 1 }).mockImplementation(() => new Promise(() => {}));
    renderApp(<><Connections /><Nav /></>, "/connections?system=a&range=1h");
    await screen.findAllByRole("link", { name: /client-001.*Firing/ });
    act(() => go("/connections?system=b&range=1h"));
    await waitFor(() => expect(requests.listFindings).toHaveBeenLastCalledWith("b", expect.anything(), expect.anything(), expect.anything()));
    expect(screen.queryByRole("link", { name: /client-001/ })).not.toBeInTheDocument();
    act(() => go("/connections?system=b&range=6h"));
    await waitFor(() => expect(requests.listFindings).toHaveBeenCalledTimes(3));
    expect(screen.queryByRole("link", { name: /client-001/ })).not.toBeInTheDocument();
  });
});

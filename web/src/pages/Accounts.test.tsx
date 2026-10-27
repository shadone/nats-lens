import { act, screen, within } from "@testing-library/react";
import { useNavigate } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { account, empty, EPOCH_AT, expiring, global, login, prod, SUPPLIERS } from "../test/fixtures/accounts";
import { renderApp } from "../test/render";
import { Accounts } from "./Accounts";

vi.mock("../api/requests", () => ({
  getAccountsAndAuth: vi.fn(),
  getLensInfo: vi.fn(() => new Promise(() => {})),
  getShellStatus: vi.fn(() => new Promise(() => {})),
}));

afterEach(() => vi.mocked(requests.getAccountsAndAuth).mockReset());

const DAY = 86_400_000;

function card(name: string) {
  return within(screen.getByRole("region", { name }));
}

function bodyRows(name: string) {
  return card(name).getAllByRole("row").slice(1);
}

function cells(row: HTMLElement) {
  return [within(row).getByRole("rowheader"), ...within(row).getAllByRole("cell")];
}

describe("Accounts", () => {
  it("says the mode, the count and where names come from, for the system of the page", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(global());
    renderApp(<Accounts />, "/accounts?system=example-global");
    expect(await screen.findByText(/^Operator mode · 3 accounts · names from the account JWTs, in the collection of/)).toBeInTheDocument();
    expect(vi.mocked(requests.getAccountsAndAuth).mock.calls[0][0]).toBe("example-global");
  });

  it("lists the accounts with their limits, the system account last", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(global());
    renderApp(<Accounts />, "/accounts?system=example-global");
    await screen.findByRole("region", { name: "Accounts and limits" });
    const rows = bodyRows("Accounts and limits");
    expect(rows.map((r) => within(r).getByRole("rowheader").querySelector("a")?.textContent)).toEqual(["APP", "SUPPLIERS", "SYS"]);

    const [name, conns, subs, disk, streams, ie, jwt] = cells(rows[0]);
    expect(within(name).getByRole("link", { name: "APP" })).toHaveAttribute("href", "/connections?account=APP&system=example-global");
    expect(conns).toHaveTextContent("412 of 2,000");
    expect(within(conns).getByRole("img")).toHaveAccessibleName("Connections: 20% of the limit");
    expect(subs).toHaveTextContent("3,120");
    expect(disk).toHaveTextContent("410 GiB of 500 GiB82% of the limit");
    // 82% of the disk limit: the warning colour.
    expect(disk.querySelector(".ac-bar-warn")).not.toBeNull();
    expect(conns.querySelector(".ac-bar-warn")).toBeNull();
    expect(streams).toHaveTextContent("23");
    expect(ie).toHaveTextContent("1 import");
    expect(jwt).toHaveTextContent("never");
  });

  it("shows an idle account by its name with its key shortened, and its JWT expiry in the warning colour", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(global());
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Accounts and limits" });
    const [name, conns, , disk, , ie, jwt] = cells(bodyRows("Accounts and limits")[1]);
    expect(name).toHaveTextContent(`SUPPLIERSADSU…RT5NH account ID ${SUPPLIERS}`);
    expect(within(name).getByRole("link", { name: "SUPPLIERS" })).toHaveAttribute("href", `/connections?account=${SUPPLIERS}`);
    expect(conns).toHaveTextContent("0 of 50");
    expect(disk).toHaveTextContent(/^JetStream off$/);
    expect(ie).toHaveTextContent("1 export · idle now");
    expect(jwt).toHaveTextContent("2026-10-29in 12 days");
    expect(jwt).toHaveClass("ac-warn");
  });

  it("calls the system account so, with no bar against an unlimited count", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(global());
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Accounts and limits" });
    const [, conns, , , , ie] = cells(bodyRows("Accounts and limits")[2]);
    expect(conns).toHaveTextContent("0 of unlimited");
    expect(within(conns).queryByRole("img")).toBeNull();
    expect(ie).toHaveTextContent("system account");
  });

  it("says an account was not looked up yet, and leaves its limits off", async () => {
    const d = global();
    d.accounts = [account("APP", { connections: 4, detailsKnown: false, imports: undefined, exports: undefined, connLimit: undefined, diskLimit: undefined, detailsAt: undefined })];
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(d);
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Accounts and limits" });
    const [, conns, , , , ie, jwt] = cells(bodyRows("Accounts and limits")[0]);
    expect(conns).toHaveTextContent(/^4$/);
    expect(ie).toHaveTextContent(/^not looked up yet$/);
    expect(ie).toHaveClass("ac-quiet");
    expect(jwt).toHaveTextContent(/^– not looked up yet$/);
    expect(jwt.querySelector("[aria-hidden]")).toHaveTextContent("–");
  });

  it("shows a dash, never 0, for counts a server did not report", async () => {
    const d = global();
    d.accounts = [account("APP", { connections: undefined, subscriptions: undefined, jsStorage: undefined, streams: undefined })];
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(d);
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Accounts and limits" });
    const [, conns, subs, disk, streams] = cells(bodyRows("Accounts and limits")[0]);
    for (const c of [conns, subs, disk, streams]) expect(c.textContent).toMatch(/^–/);
  });

  it("lists failed logins, the frequent one in the warning colour, with the board's sentence", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(global());
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Failed logins, 24 h" });
    const rows = bodyRows("Failed logins, 24 h");
    expect(rows[0]).toHaveClass("ac-hot");
    expect(rows[0]).toHaveTextContent("label-printeraccount APP · frequent, about 60 an hour10.20.4.31User Authentication Revoked1,44014:29");
    expect(rows[1]).not.toHaveClass("ac-hot");
    expect(rows[1]).toHaveTextContent("(no user)203.0.113.12Authentication Timeout9613:49");
    expect(card("Failed logins, 24 h").getByText(/revoked credentials every minute/)).toBeInTheDocument();
    expect(card("Failed logins, 24 h").queryByText(/Event capture stopped/)).toBeNull();
  });

  it("says when event capture had gaps and when the history is shorter than 24 hours", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({ ...global(), captureGaps: true, failedLoginsWindowStart: EPOCH_AT - 3 * 3_600_000 });
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Failed logins, 24 h" });
    const c = card("Failed logins, 24 h");
    expect(c.getByText("Event capture stopped for part of this window, so the counts can be low.")).toBeInTheDocument();
    expect(c.getByText("The history starts at 17 Oct 11:30 UTC, so fewer than 24 hours are counted.")).toBeInTheDocument();
  });

  it("lists what expires soonest with how long is left", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(global());
    renderApp(<Accounts />, "/accounts?system=example-global");
    await screen.findByRole("region", { name: "Expiring soon" });
    const rows = bodyRows("Expiring soon");
    expect(rows.map((r) => r.textContent)).toEqual([
      "pos-terminal user JWTaccount APP · 3 connections2026-10-225 days",
      "SUPPLIERS account JWT2026-10-2912 days",
      "nats-eu-0 certificate, client listener2026-11-0216 days",
    ]);
    expect(within(rows[2]).getByRole("link", { name: "nats-eu-0" })).toHaveAttribute("href", "/servers/nats-eu-0?system=example-global");
    expect(cells(rows[2])[2]).toHaveClass("ac-warn");
    expect(card("Expiring soon").getByText("26 more expire later; 3 never expire.")).toBeInTheDocument();
    expect(card("Expiring soon").queryByText(/In config mode/)).toBeNull();
  });

  it("colours an expired row critical", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({
      ...global(),
      expiring: [expiring({ kind: "certificate", server: "nats-eu-1", listener: "gateway", expires: EPOCH_AT - 2 * DAY, expired: true })],
      expiringTotal: 1,
    });
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Expiring soon" });
    const c = cells(bodyRows("Expiring soon")[0]);
    expect(c[2]).toHaveTextContent("expired");
    expect(c[2]).toHaveClass("ac-crit");
  });

  it("in config mode shows dashes for JWTs, the empty states and the config mode line", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(prod());
    renderApp(<Accounts />, "/accounts?system=example-prod");
    expect(await screen.findByText(/^Config mode · 1 account · names from the server configuration/)).toBeInTheDocument();
    const [name, conns, , disk, , ie, jwt] = cells(bodyRows("Accounts and limits")[0]);
    expect(name).toHaveTextContent(/^APP$/);
    expect(conns).toHaveTextContent(/^38$/);
    expect(disk).toHaveTextContent(/^2 GiB$/);
    expect(ie).toHaveTextContent("none");
    expect(jwt).toHaveTextContent(/^– no account JWTs in config mode$/);
    expect(card("Failed logins, 24 h").getByText("No failed logins in these 24 hours.")).toBeInTheDocument();
    expect(card("Failed logins, 24 h").queryByRole("table")).toBeNull();
    expect(card("Failed logins, 24 h").queryByText(/revoked credentials/)).toBeNull();
    expect(card("Expiring soon").getByText("Nothing expires within 30 days.")).toBeInTheDocument();
    expect(card("Expiring soon").getByText("6 expire later.")).toBeInTheDocument();
    expect(card("Expiring soon").getByText(/^In config mode there are no JWTs/)).toBeInTheDocument();
  });

  it("says the range holds no collection, without the cards", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(empty());
    renderApp(<Accounts />, "/accounts");
    expect(await screen.findByText("No collection in this range.")).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Accounts and limits" })).toBeNull();
    expect(screen.queryByRole("region", { name: "Failed logins, 24 h" })).toBeNull();
    expect(screen.getByText(/in the newest collection of the range\./)).toBeInTheDocument();
  });

  it("says how many accounts and failed-login groups were cut", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({ ...global(), accountsTotal: 612, failedLoginGroupsTotal: 140, failedLoginsTotal: 2_000 });
    renderApp(<Accounts />, "/accounts");
    expect(await screen.findByText("The first 3 of 612 accounts are listed.")).toBeInTheDocument();
    expect(screen.getByText("The 2 most frequent of 140 groups are listed; 2,000 failed logins in all.")).toBeInTheDocument();
  });

  it("says when a collection reports no account", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({ ...prod(), accounts: [], accountsTotal: 0 });
    renderApp(<Accounts />, "/accounts");
    expect(await screen.findByText("No account reported in this collection.")).toBeInTheDocument();
  });
});

describe("Accounts' JetStream disk cell", () => {
  async function diskOf(o: Parameters<typeof account>[1]) {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({ ...global(), accounts: [account("APP", { connections: 4, hasJwt: true, ...o })] });
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Accounts and limits" });
    return cells(bodyRows("Accounts and limits")[0])[3];
  }

  it("says JetStream off when the JWT disables it", async () => {
    expect(await diskOf({ diskLimit: 0, jetstreamEnabled: false, jsStorage: undefined })).toHaveTextContent(/^JetStream off$/);
  });

  it("says memory only when JetStream is enabled without file storage", async () => {
    expect(await diskOf({ diskLimit: 0, jetstreamEnabled: true, jsStorage: 0 })).toHaveTextContent(/^memory only$/);
  });

  it("shows the use against the limit when file storage is allowed", async () => {
    expect(await diskOf({ diskLimit: 1024 ** 3, jetstreamEnabled: true, jsStorage: 512 * 1024 ** 2 })).toHaveTextContent(/^512 MiB of 1 GiB$/);
  });

  it("shows the use alone without a known JWT", async () => {
    expect(await diskOf({ diskLimit: undefined, jetstreamEnabled: undefined, jsStorage: 0, hasJwt: false })).toHaveTextContent(/^0 B$/);
  });
});

describe("Accounts' warnings beyond colour", () => {
  it("draws no fill at 0 of a limit", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(global());
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Accounts and limits" });
    const bar = within(cells(bodyRows("Accounts and limits")[1])[1]).getByRole("img", { name: "Connections: 0% of the limit" });
    expect(bar.firstElementChild).not.toHaveClass("ac-bar-some");
    const app = within(cells(bodyRows("Accounts and limits")[0])[1]).getByRole("img");
    expect(app.firstElementChild).toHaveClass("ac-bar-some");
  });

  it("says an account over its limit is over it, with the real share, in the critical colour", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({ ...prod(), operatorMode: true, accounts: [account("APP", { connections: 75, connLimit: 50, hasJwt: true })] });
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Accounts and limits" });
    const conns = cells(bodyRows("Accounts and limits")[0])[1];
    expect(within(conns).getByRole("img")).toHaveAccessibleName("Connections: 150% of the limit, over the limit");
    expect(within(conns).getByRole("img").firstElementChild).toHaveClass("ac-bar-crit");
    expect(conns).toHaveTextContent("75 of 50150% of the limit, over the limit");
    expect(conns.firstElementChild).toHaveClass("ac-crit");
  });

  it("counts the leaf nodes of the server that holds the most against the connection limit, and names them under the count", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({
      ...prod(),
      operatorMode: true,
      accounts: [account("APP", { connections: 85, leafnodes: 11, limitLeafnodes: 8, limitLeafnodesServer: "nats-0", connLimit: 100, hasJwt: true })],
    });
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Accounts and limits" });
    const conns = cells(bodyRows("Accounts and limits")[0])[1];
    expect(conns.firstElementChild).toHaveTextContent("93 of 100");
    expect(within(conns).getByRole("img")).toHaveAccessibleName("Connections: 93% of the limit");
    expect(conns).toHaveTextContent("includes 8 leaf nodes on nats-0 · 11 leaf nodes in all");
  });

  it("says a warning count's share in words, and nothing below 80%", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(global());
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Accounts and limits" });
    const [, conns, , disk] = cells(bodyRows("Accounts and limits")[0]);
    expect(disk.querySelector(".ac-limit-note")).toHaveTextContent("82% of the limit");
    expect(conns.querySelector(".ac-limit-note")).toBeNull();
  });

  it("marks a frequent failed-login group in words next to the user, for screen readers too", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(global());
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Failed logins, 24 h" });
    const [hot, quiet] = bodyRows("Failed logins, 24 h");
    expect(within(hot).getByRole("rowheader")).toHaveTextContent("about 60 an hour");
    expect(within(hot).getByRole("rowheader").querySelector(".sr-only")).toHaveTextContent("frequent");
    expect(within(quiet).getByRole("rowheader")).not.toHaveTextContent("an hour");
  });
});

describe("Accounts on the first collection", () => {
  it("says no failed logins since the history starts, not in 24 hours", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({ ...prod(), failedLoginsWindowStart: EPOCH_AT });
    renderApp(<Accounts />, "/accounts");
    expect(await screen.findByText("No failed logins since 17 Oct 14:30 UTC.")).toBeInTheDocument();
    expect(screen.queryByText("No failed logins in these 24 hours.")).toBeNull();
  });

  it("says where the history starts and rates the attempts over at least an hour", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({
      ...global(),
      failedLoginsWindowStart: EPOCH_AT,
      failedLogins: [login("label-printer", "10.20.4.31", "User Authentication Revoked", 70), login("", "203.0.113.12", "Authentication Timeout", 8)],
    });
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Failed logins, 24 h" });
    expect(card("Failed logins, 24 h").getByText("The history starts at 17 Oct 14:30 UTC, so fewer than 24 hours are counted.")).toBeInTheDocument();
    const [hot, quiet] = bodyRows("Failed logins, 24 h");
    expect(hot).toHaveClass("ac-hot");
    expect(hot).toHaveTextContent("70 since 14:30 UTC");
    expect(quiet).not.toHaveClass("ac-hot");
  });
});

describe("Accounts in config mode without a lookup", () => {
  it("says not looked up yet in Imports / exports, visibly", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({
      ...prod(),
      accounts: [account("APP", { connections: 16, streams: 3, detailsKnown: false, imports: undefined, exports: undefined, detailsAt: undefined })],
    });
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Accounts and limits" });
    const [, , , , , ie, jwt] = cells(bodyRows("Accounts and limits")[0]);
    expect(ie).toHaveTextContent(/^not looked up yet$/);
    expect(ie.querySelector("[aria-hidden]")).toBeNull();
    expect(jwt).toHaveTextContent(/^– no account JWTs in config mode$/);
  });
});

describe("Accounts without captured events", () => {
  it("does not say there were no failed logins, and says capture is not running", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({ ...prod(), eventsCaptured: false, captureGaps: false });
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Failed logins, 24 h" });
    const c = card("Failed logins, 24 h");
    expect(c.getByText("Failed logins are not shown: no event was stored up to this collection, so event capture was not running for this system then.")).toBeInTheDocument();
    expect(c.queryByText(/No failed logins/)).toBeNull();
    expect(c.queryByText(/revoked credentials/)).toBeNull();
    expect(c.queryByText(/Event capture stopped/)).toBeNull();
    expect(c.queryByRole("table")).toBeNull();
  });

  it("still says there were none when events were captured", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(prod());
    renderApp(<Accounts />, "/accounts");
    expect(await screen.findByText("No failed logins in these 24 hours.")).toBeInTheDocument();
    expect(screen.queryByText(/event capture was not running/)).toBeNull();
  });
});

describe("Accounts' certificates read from an older collection", () => {
  it("says when the row was read and that the server did not answer now", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({
      ...global(),
      expiring: [
        expiring({ kind: "certificate", server: "nats-us-4", listener: "client", expires: EPOCH_AT + 16 * DAY, seenAt: EPOCH_AT - 70 * 60_000 }),
        expiring({ kind: "certificate", server: "nats-eu-0", listener: "client", expires: EPOCH_AT + 17 * DAY }),
      ],
      expiringTotal: 2,
    });
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Expiring soon" });
    const [old, fresh] = bodyRows("Expiring soon");
    expect(within(old).getByRole("rowheader")).toHaveTextContent("nats-us-4 certificate, client listenerread at 13:20 UTC; no answer from the server since");
    expect(within(fresh).getByRole("rowheader")).toHaveTextContent(/^nats-eu-0 certificate, client listener$/);
  });
});

describe("Accounts review round 2", () => {
  it("says not looked up yet once in an operator-mode row: visibly in Imports / exports, for screen readers in JWT expires", async () => {
    const d = global();
    d.accounts[0] = { ...d.accounts[0], detailsKnown: false, imports: undefined, exports: undefined, hasJwt: false, jwtExpires: undefined };
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(d);
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Accounts and limits" });
    const row = bodyRows("Accounts and limits")[0];
    const seen = row.cloneNode(true) as HTMLElement;
    seen.querySelectorAll(".sr-only").forEach((n) => n.remove());
    expect(row.textContent!.match(/not looked up yet/g)).toHaveLength(2);
    expect(seen.textContent!.match(/not looked up yet/g) ?? []).toHaveLength(1);
  });

  it("hides the cut line, the history line and the gap line without captured events", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({ ...global(), eventsCaptured: false, captureGaps: true, failedLoginsWindowStart: EPOCH_AT - 2 * 3_600_000, failedLoginGroupsTotal: 5 });
    renderApp(<Accounts />, "/accounts");
    const c = within(await screen.findByRole("region", { name: "Failed logins, 24 h" }));
    expect(c.getByText(/^Failed logins are not shown/)).toBeInTheDocument();
    expect(c.queryByText(/most frequent of/)).toBeNull();
    expect(c.queryByText(/history starts/)).toBeNull();
    expect(c.queryByText(/Event capture stopped/)).toBeNull();
  });

  it("keeps the quiet cue lines muted: no tone class on the limit line or the rate", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({ ...prod(), operatorMode: true, accounts: [account("APP", { connections: 75, connLimit: 50, hasJwt: true })] });
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Accounts and limits" });
    const note = document.querySelector(".ac-limit-note")!;
    expect(note.className).toBe("ac-limit-note");
  });

  it("keeps the rate cue as plain text in the muted line", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue(global());
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Failed logins, 24 h" });
    const small = within(bodyRows("Failed logins, 24 h")[0]).getByRole("rowheader").querySelector("small")!;
    expect(small.querySelector(".ac-warn, .ac-crit")).toBeNull();
  });
});

function Go({ to }: { to: string }) {
  const nav = useNavigate();
  return (
    <button type="button" onClick={() => nav(to)}>
      go
    </button>
  );
}

function SwitchSystem() {
  const nav = useNavigate();
  return (
    <button type="button" onClick={() => nav("/accounts?system=example-prod")}>
      switch
    </button>
  );
}

describe("Accounts on a system switch", () => {
  it("does not show the previous system's accounts while the new system's load", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValueOnce(global()).mockReturnValueOnce(new Promise(() => {}));
    renderApp(
      <>
        <Accounts />
        <SwitchSystem />
      </>,
      "/accounts?system=example-global",
    );
    await screen.findByText(/^Operator mode · 3 accounts/);
    await act(async () => screen.getByRole("button", { name: "switch" }).click());
    expect(vi.mocked(requests.getAccountsAndAuth).mock.calls.at(-1)![0]).toBe("example-prod");
    expect(screen.queryByText(/^Operator mode · 3 accounts/)).toBeNull();
    expect(screen.queryByRole("region", { name: "Accounts and limits" })).toBeNull();
  });

  it("keeps the same system's answer while a new range loads", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValueOnce(global()).mockReturnValueOnce(new Promise(() => {}));
    renderApp(
      <>
        <Accounts />
        <Go to="/accounts?system=example-global&range=1h" />
      </>,
      "/accounts?system=example-global",
    );
    await screen.findByText(/^Operator mode · 3 accounts/);
    await act(async () => screen.getByRole("button", { name: "go" }).click());
    expect(vi.mocked(requests.getAccountsAndAuth).mock.calls.length).toBe(2);
    expect(screen.getByText(/^Operator mode · 3 accounts/)).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Accounts and limits" })).toBeInTheDocument();
  });
});

describe("Accounts' lines under a cell", () => {
  it("keeps each part on one line, so a line breaks only between parts", async () => {
    vi.mocked(requests.getAccountsAndAuth).mockResolvedValue({
      ...global(),
      accounts: [account("APP", { connections: 75, connLimit: 50, hasJwt: true })],
      expiring: [expiring({ kind: "certificate", server: "nats-us-4", listener: "client", expires: EPOCH_AT + 16 * DAY, seenAt: EPOCH_AT - 70 * 60_000 })],
      expiringTotal: 1,
    });
    renderApp(<Accounts />, "/accounts");
    await screen.findByRole("region", { name: "Expiring soon" });
    const parts = (el: Element) => [...el.querySelectorAll(".ac-nw")].map((n) => n.textContent);
    expect(parts(document.querySelector(".ac-limit-note")!)).toEqual(["150% of the limit", "over the limit"]);
    const login = within(bodyRows("Failed logins, 24 h")[0]).getByRole("rowheader").querySelector("small")!;
    expect(parts(login)).toEqual(["account APP", "frequent, about 60 an hour"]);
    const cert = within(bodyRows("Expiring soon")[0]).getByRole("rowheader").querySelector("small")!;
    expect(parts(cert)).toEqual(["read at 13:20 UTC", "no answer from the server since"]);
  });
});

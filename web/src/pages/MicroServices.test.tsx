import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useNavigate } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import * as systems from "../api/systems";
import { account, global, noCollection, none, orders, prod, seenOnly, service, SUPPLIERS } from "../test/fixtures/micro";
import { renderApp } from "../test/render";
import { MicroServices } from "./MicroServices";

vi.mock("../api/requests", () => ({
  getMicroServices: vi.fn(),
  getLensInfo: vi.fn(() => Promise.resolve({ identityMode: "request", version: "dev", cluster: "c", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: true })),
  getShellStatus: vi.fn(() => Promise.reject(new Error("no shell in this test"))),
}));
vi.mock("../api/systems", async (orig) => ({ ...(await orig<typeof import("../api/systems")>()), useAccounts: vi.fn(() => ({ data: undefined })) }));
const getMicro = vi.mocked(requests.getMicroServices);
const useAccounts = vi.mocked(systems.useAccounts);

afterEach(() => {
  getMicro.mockReset();
  useAccounts.mockImplementation(() => ({ data: undefined }) as ReturnType<typeof systems.useAccounts>);
});

function cards() {
  return within(screen.getByRole("group", { name: "Services" })).getAllByRole("button");
}

function card(name: string) {
  return cards().find((c) => c.querySelector(".mi-name")?.textContent === name) as HTMLElement;
}

function detail() {
  return within(screen.getByRole("region", { name: /endpoints$|service$/ }));
}

function rowOf(t: ReturnType<typeof within>, header: string) {
  return t.getByRole("rowheader", { name: header }).closest("tr") as HTMLElement;
}

function cells(t: ReturnType<typeof within>, header: string) {
  return within(rowOf(t, header))
    .getAllByRole("cell")
    .map((c) => c.textContent);
}

function location() {
  return screen.getByTestId("location").textContent ?? "";
}

function Go({ to }: { to: string }) {
  const nav = useNavigate();
  return (
    <button type="button" onClick={() => nav(to)}>
      go
    </button>
  );
}

describe("Micro services", () => {
  it("says what the page shows and names the collection, for the system and scope of the page", async () => {
    getMicro.mockResolvedValue(global());
    renderApp(<MicroServices />, "/micro-services?system=example-global&account=APP");
    await screen.findByRole("group", { name: "Services" });
    const line = screen.getByText((_, el) => el?.classList.contains("aside") === true);
    expect(line).toHaveTextContent("Services built on the NATS micro framework, found through $SRV.INFO and measured through $SRV.STATS. In the collection of 14:30:00 UTC.");
    expect(getMicro.mock.calls[0][0]).toBe("example-global");
    expect(getMicro.mock.calls[0][2]).toBe("APP");
  });

  it("says only that the range holds no collection", async () => {
    getMicro.mockResolvedValue(noCollection());
    renderApp(<MicroServices />, "/micro-services");
    expect(await screen.findByText("No collection in this range.")).toBeInTheDocument();
    expect(screen.queryByRole("group", { name: "Services" })).toBeNull();
  });

  it("shows a card per service with the board's figures and notes, the first chosen", async () => {
    getMicro.mockResolvedValue(global());
    renderApp(<MicroServices />, "/micro-services?system=example-global");
    await screen.findByRole("group", { name: "Services" });
    expect(cards().map((c) => c.querySelector(".mi-name")?.textContent)).toEqual(["orders", "pricing", "inventory"]);
    expect(card("orders")).toHaveTextContent("orders version 1.4.2 Order intake and lookup 3 instances 161.7 req/s <0.1% errors orders.get slower than usual since 08:12");
    expect(card("pricing")).toHaveTextContent("2.0.0 / 1.9.3");
    expect(card("pricing").querySelector(".mi-note")).toHaveTextContent("mixed versions: 3 on 2.0.0, 1 on 1.9.3");
    expect(card("pricing").querySelector(".mi-note")).toHaveClass("mi-note-warn");
    expect(card("inventory").querySelector(".mi-note")).toHaveTextContent("healthy");
    expect(card("inventory").querySelector(".mi-note")).not.toHaveClass("mi-note-warn");
    expect(card("orders")).toHaveAttribute("aria-pressed", "true");
    expect(card("pricing")).toHaveAttribute("aria-pressed", "false");
    // One account: the cards do not name it.
    expect(card("orders").querySelector(".mi-acc")).toBeNull();
  });

  it("lists the chosen service's endpoints with the board's columns, the slow one in the warning colours", async () => {
    getMicro.mockResolvedValue(global());
    renderApp(<MicroServices />, "/micro-services?system=example-global");
    await screen.findByRole("group", { name: "Services" });
    expect(screen.getByRole("heading", { level: 2 })).toHaveTextContent("orders endpoints");
    const t = within(detail().getAllByRole("table")[0]);
    expect(t.getAllByRole("columnheader").map((h) => h.textContent)).toEqual(["Endpoint", "Subject", "Queue group", "Req/s", "Avg time", "Errors", "Last error"]);
    expect(cells(t, "get")).toEqual(["orders.get.*", "q", "118.4", "41 ms", "0%", "avg time 10× its 7-day level since 08:12"]);
    expect(rowOf(t, "get")).toHaveClass("mi-warn");
    expect(cells(t, "create")).toEqual(["orders.create", "q", "42.1", "3.8 ms", "0.1%", "409 duplicate order id on instance ORD1"]);
    expect(rowOf(t, "create")).not.toHaveClass("mi-warn");
    expect(cells(t, "cancel")[5]).toBe("– no error reported");
    // A literal subject opens the Subjects tester on it in the service's account; a wildcard one is text.
    expect(within(rowOf(t, "create")).getByRole("link", { name: "orders.create" })).toHaveAttribute("href", "/subjects?account=APP&subject=orders.create&system=example-global");
    expect(within(rowOf(t, "get")).queryByRole("link")).toBeNull();
  });

  it("lists the instances with links to their connections and servers", async () => {
    getMicro.mockResolvedValue(global());
    renderApp(<MicroServices />, "/micro-services?system=example-global");
    await screen.findByRole("group", { name: "Services" });
    const t = within(detail().getAllByRole("table")[1]);
    expect(t.getAllByRole("columnheader").map((h) => h.textContent)).toEqual(["Instance", "Version", "Started", "Connection", "Server"]);
    expect(cells(t, "ORD1")).toEqual(["1.4.2", "15 Oct 07:30", "order-service", "nats-eu-1"]);
    expect(within(rowOf(t, "ORD1")).getByRole("link", { name: "order-service" })).toHaveAttribute("href", "/connections?name=order-service&server=nats-eu-1&system=example-global");
    expect(within(rowOf(t, "ORD1")).getByRole("link", { name: "nats-eu-1" })).toHaveAttribute("href", "/servers/nats-eu-1?system=example-global");
  });

  it("shows a missing instance with its times, in the warning colours", async () => {
    const inv = service({
      note: "missing",
      instances: 1,
      instanceList: [service().instanceList[0]],
      missing: [{ id: "INV1", since: Date.parse("2026-10-17T10:05:00Z"), lastSeen: Date.parse("2026-10-17T10:04:30Z"), connectionName: "inventory-service", stillSubscribed: true }],
    });
    getMicro.mockResolvedValue(global({ services: [inv] }));
    renderApp(<MicroServices />, "/micro-services");
    await screen.findByRole("group", { name: "Services" });
    expect(card("inventory").querySelector(".mi-note")).toHaveTextContent("instance missing: INV1 since 10:05");
    const t = within(detail().getAllByRole("table")[1]);
    expect(cells(t, "INV1")).toEqual(["– not known", "– not known", "inventory-service", "– not known", "missing since 10:05, last answered 10:04; still subscribed, so registered but not answering"]);
    expect(rowOf(t, "INV1")).toHaveClass("mi-warn");
  });

  it("chooses a card by click and by keyboard, and keeps it in the address", async () => {
    getMicro.mockResolvedValue(global());
    renderApp(<MicroServices />, "/micro-services?system=example-global&range=6h");
    await screen.findByRole("group", { name: "Services" });
    await userEvent.click(card("pricing"));
    expect(card("pricing")).toHaveAttribute("aria-pressed", "true");
    expect(card("orders")).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByRole("heading", { level: 2 })).toHaveTextContent("pricing endpoints");
    expect(location()).toBe("/micro-services?system=example-global&range=6h&service=APP%2Fpricing");
    card("inventory").focus();
    await userEvent.keyboard("{Enter}");
    expect(card("inventory")).toHaveAttribute("aria-pressed", "true");
    expect(location()).toContain("service=APP%2Finventory");
    await userEvent.keyboard(" ");
    expect(card("inventory")).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByTestId("navigation")).toHaveTextContent("REPLACE");
  });

  it("opens on the service the address names, and says when it names one not listed", async () => {
    getMicro.mockResolvedValue(global());
    const { unmount } = renderApp(<MicroServices />, "/micro-services?service=APP%2Finventory");
    await screen.findByRole("group", { name: "Services" });
    expect(card("inventory")).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("heading", { level: 2 })).toHaveTextContent("inventory endpoints");
    unmount();
    renderApp(<MicroServices />, "/micro-services?service=APP%2Fshipping");
    await screen.findByRole("group", { name: "Services" });
    expect(screen.getByText("No service APP/shipping in this collection; showing orders.")).toBeInTheDocument();
    expect(card("orders")).toHaveAttribute("aria-pressed", "true");
  });

  it("says why an asked account without services gave no answer", async () => {
    getMicro.mockResolvedValue(global());
    renderApp(<MicroServices />, "/micro-services");
    await screen.findByRole("group", { name: "Services" });
    const note = screen.getAllByRole("note").find((n) => /SUPPLIERS/.test(n.textContent ?? "")) as HTMLElement;
    expect(note).toHaveTextContent(
      `SUPPLIERS (account ID ${SUPPLIERS}) asked, no answer. The server refused $SRV.STATS (nats: Permissions Violation for Publish to "$SRV.STATS"): the account's user needs to publish on $SRV.INFO and $SRV.STATS and subscribe on _INBOX.>.`,
    );
  });

  it("shows a service seen in subscriptions only with what is known and how to see more", async () => {
    getMicro.mockResolvedValue(prod());
    renderApp(<MicroServices />, "/micro-services?system=example-prod");
    await screen.findByRole("group", { name: "Services" });
    expect(card("catalog")).toHaveTextContent("catalog seen in subscriptions only 2 instances – not asked req/s – not asked errors list the account under accounts for versions, endpoints and timings");
    expect(screen.getByRole("heading", { level: 2 })).toHaveTextContent("catalog service");
    expect(detail().getByText(/^nats-lens does not ask APP, so versions, endpoints and timings are not known\./)).toBeInTheDocument();
    expect(detail().getByText(/^To see more, list APP under/)).toHaveTextContent("To see more, list APP under accounts in the configuration");
    const t = within(detail().getByRole("table"));
    expect(cells(t, "CAT0")).toEqual(["– not asked", "– not asked", "catalog-api", "nats-0"]);
  });

  it("names the account on each card when the services span accounts and the page is not scoped", async () => {
    getMicro.mockResolvedValue(global({ services: [orders(), service({ account: "SHOP", name: "inventory" })] }));
    renderApp(<MicroServices />, "/micro-services");
    await screen.findByRole("group", { name: "Services" });
    expect(card("inventory").querySelector(".mi-acc")).toHaveTextContent("account SHOP");
    expect(screen.getByRole("heading", { level: 2 })).toHaveTextContent("orders endpoints in APP");
  });

  it("says when no service is known and subscriptions were not collected", async () => {
    getMicro.mockResolvedValue(none());
    renderApp(<MicroServices />, "/micro-services?system=example-staging");
    expect(await screen.findByText((_, el) => el?.classList.contains("mi-wrap") === true)).toHaveTextContent(
      "No micro service known: no account is listed under accounts, and no subscription list within 24 hours before this collection.",
    );
    expect(screen.queryByText(/Subscriptions not collected/)).toBeNull();
  });

  it("says subscriptions were not collected above the services of asked accounts", async () => {
    getMicro.mockResolvedValue(global({ subscriptionsAt: undefined }));
    renderApp(<MicroServices />, "/micro-services");
    await screen.findByRole("group", { name: "Services" });
    expect(screen.getAllByRole("note")[0]).toHaveTextContent(/^Subscriptions not collected\. No subscription list within 24 hours before this collection: only the services of accounts listed under accounts are shown/);
  });

  it("says why a chosen service of an asked account has no endpoints", async () => {
    const s = service({ account: "SHOP", note: "noAnswer", answered: false, instanceSource: "lastAnswered", endpoints: [], requestsPerSecond: undefined, errorShare: undefined });
    getMicro.mockResolvedValue(global({ services: [s], accounts: [account({ account: "SHOP", services: 1, stats: { state: "timeout", replies: 0, error: "" } })] }));
    renderApp(<MicroServices />, "/micro-services");
    await screen.findByRole("group", { name: "Services" });
    expect(card("inventory")).toHaveTextContent("2 instances – not observed req/s – not observed errors no answer to $SRV.STATS: no reply within the collect window");
    expect(detail().getByText("No endpoints known: none of its instances answered $SRV.STATS in this collection (no reply within the collect window).")).toBeInTheDocument();
    // Its instances are those that last answered within the hour, and the heading says so.
    expect(detail().getByRole("heading", { level: 3 })).toHaveTextContent("Instances that last answered within the hour before this collection");
  });

  it("names an account the response leaves unnamed from the header's account list", async () => {
    useAccounts.mockImplementation(() => ({ data: [{ account: SUPPLIERS, name: "SUPPLIERS-LIST", isSystem: false }] }) as unknown as ReturnType<typeof systems.useAccounts>);
    const d = global();
    d.accounts[0] = { ...d.accounts[0], accountName: "" };
    getMicro.mockResolvedValue(d);
    renderApp(<MicroServices />, "/micro-services");
    await screen.findByRole("group", { name: "Services" });
    expect(screen.getAllByRole("note").find((n) => /asked, no answer/.test(n.textContent ?? ""))).toHaveTextContent(/^SUPPLIERS-LIST \(account ID /);
  });

  it("says a service of another account is not in the scoped one", async () => {
    getMicro.mockResolvedValue(global({ services: [service({ account: "SHOP", name: "inventory" })], accounts: [account({ account: "SHOP", accountName: "Shop" })] }));
    renderApp(<MicroServices />, "/micro-services?account=SHOP&service=APP%2Forders");
    await screen.findByRole("group", { name: "Services" });
    expect(screen.getByText("orders of APP is not in account Shop; showing inventory.")).toBeInTheDocument();
  });

  it("adds a Note column to the instances only when one is missing", async () => {
    getMicro.mockResolvedValue(global());
    renderApp(<MicroServices />, "/micro-services");
    await screen.findByRole("group", { name: "Services" });
    expect(within(detail().getAllByRole("table")[1]).queryByRole("columnheader", { name: "Note" })).toBeNull();
  });

  it("sets the configuration key accounts in mono", async () => {
    getMicro.mockResolvedValue(prod());
    renderApp(<MicroServices />, "/micro-services");
    await screen.findByRole("group", { name: "Services" });
    expect(card("catalog").querySelector(".mi-note .mono")).toHaveTextContent("accounts");
    expect(detail().getByText(/^To see more/).querySelector(".mono")).toHaveTextContent("accounts");
    expect(screen.getByText(/^Each account listed under/).querySelector(".mono")).toHaveTextContent("accounts");
  });

  it("words a share just above 1% so it agrees with its colour", async () => {
    const s = service({ errorShare: 0.01004, endpoints: [{ ...service().endpoints[0], errorShare: 0.01004, errorsHigh: true }] });
    getMicro.mockResolvedValue(global({ services: [s] }));
    renderApp(<MicroServices />, "/micro-services");
    await screen.findByRole("group", { name: "Services" });
    expect(card("inventory").querySelector(".mi-crit")).toHaveTextContent(">1.0%");
    expect(cells(within(detail().getAllByRole("table")[0]), "reserve")[4]).toBe(">1.0%");
  });

  it("heads instances from the subscription list as such, also in an asked account without an answer", async () => {
    const s = service({ note: "noAnswer", answered: false, instanceSource: "subscriptions", endpoints: [], requestsPerSecond: undefined, errorShare: undefined });
    getMicro.mockResolvedValue(global({ services: [s], accounts: [account({ services: 1, stats: { state: "timeout", replies: 0, error: "" } })] }));
    renderApp(<MicroServices />, "/micro-services");
    await screen.findByRole("group", { name: "Services" });
    expect(detail().getByRole("heading", { level: 3 })).toHaveTextContent("Instances seen in the subscription list");
  });

  it("keeps backticks in server and client text as they are", async () => {
    const s = service({ account: "SHOP", note: "noAnswer", answered: false, endpoints: [], requestsPerSecond: undefined, errorShare: undefined });
    getMicro.mockResolvedValue(global({ services: [s], accounts: [account({ account: "SHOP", services: 1, stats: { state: "error", replies: 0, error: "nats: bad `x` reply" } })] }));
    const { unmount } = renderApp(<MicroServices />, "/micro-services");
    await screen.findByRole("group", { name: "Services" });
    expect(card("inventory").querySelector(".mi-note")).toHaveTextContent("the request failed: nats: bad `x` reply");
    unmount();
    const d = prod();
    d.services = [seenOnly({ connectionNames: ["cat`a`log"] })];
    getMicro.mockResolvedValue(d);
    renderApp(<MicroServices />, "/micro-services");
    await screen.findByRole("group", { name: "Services" });
    expect(screen.getByText(/^nats-lens does not ask/)).toHaveTextContent("on cat`a`log.");
  });

  it("reads a ?service= without an account part in the scoped account", async () => {
    getMicro.mockResolvedValue(global({ services: [service({ account: "SHOP", name: "inventory" })], accounts: [account({ account: "SHOP", accountName: "Shop" })] }));
    renderApp(<MicroServices />, "/micro-services?account=SHOP&service=orders");
    await screen.findByRole("group", { name: "Services" });
    expect(screen.getByText("No service orders in Shop in this collection; showing inventory.")).toBeInTheDocument();
  });

  it("lists every subject of an endpoint name, each literal one a link", async () => {
    const s = service({ endpoints: [{ ...service().endpoints[0], name: "create", subject: "orders.create", subjects: ["orders.create", "orders.create.v2", "orders.*.v3"] }] });
    getMicro.mockResolvedValue(global({ services: [s] }));
    renderApp(<MicroServices />, "/micro-services?system=example-global");
    await screen.findByRole("group", { name: "Services" });
    const row = rowOf(within(detail().getAllByRole("table")[0]), "create");
    expect(within(row).getAllByRole("cell")[0]).toHaveTextContent("orders.create, orders.create.v2, orders.*.v3");
    expect(within(row).getByRole("link", { name: "orders.create.v2" })).toHaveAttribute("href", "/subjects?account=APP&subject=orders.create.v2&system=example-global");
    expect(within(row).getAllByRole("link").map((a) => a.textContent)).toEqual(["orders.create", "orders.create.v2"]);
  });

  it("shows a dash heard as no subject for an endpoint without subjects", async () => {
    const s = service({ endpoints: [{ ...service().endpoints[0], subject: "", subjects: [] }] });
    getMicro.mockResolvedValue(global({ services: [s] }));
    renderApp(<MicroServices />, "/micro-services");
    await screen.findByRole("group", { name: "Services" });
    expect(cells(within(detail().getAllByRole("table")[0]), "reserve")[0]).toBe("– no subject reported");
  });

  it("never shows another account's services while the scoped ones load", async () => {
    getMicro.mockImplementation(async (_s, _r, acc) => (acc === "APP" ? global() : new Promise(() => {})));
    renderApp(
      <>
        <Go to="/micro-services?account=SHOP" />
        <MicroServices />
      </>,
      "/micro-services?account=APP",
    );
    await screen.findByRole("group", { name: "Services" });
    await userEvent.click(screen.getByRole("button", { name: "go" }));
    await waitFor(() => expect(screen.queryByRole("group", { name: "Services" })).toBeNull());
    expect(screen.getByText("Loading micro services…")).toBeInTheDocument();
  });
});

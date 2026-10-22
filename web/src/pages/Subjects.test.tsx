import { Code, ConnectError } from "@connectrpc/connect";
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import type { SubjectMatches } from "../api/subjects";
import { coreMatch, global, matches, noCollection, published, row, streamMatch } from "../test/fixtures/subjects";
import { renderApp } from "../test/render";
import { Subjects } from "./Subjects";

vi.mock("../api/requests", () => ({
  getSubjects: vi.fn(),
  matchSubject: vi.fn(),
  getLensInfo: vi.fn(() => Promise.resolve({ identityMode: "request", version: "dev", cluster: "c", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: true })),
  getShellStatus: vi.fn(() => Promise.reject(new Error("no shell in this test"))),
}));
const getSubjects = vi.mocked(requests.getSubjects);
const matchSubject = vi.mocked(requests.matchSubject);

afterEach(() => {
  getSubjects.mockReset();
  matchSubject.mockReset();
});

function table() {
  return within(screen.getByRole("region", { name: "Subject patterns" }));
}

function rowOf(pattern: string) {
  return table().getByRole("rowheader", { name: pattern }).closest("tr") as HTMLElement;
}

function cells(pattern: string) {
  return within(rowOf(pattern))
    .getAllByRole("cell")
    .map((c) => c.textContent);
}

function field() {
  return screen.getByRole("textbox", { name: "Test a subject" });
}

function location() {
  return screen.getByTestId("location").textContent ?? "";
}

describe("Subjects", () => {
  it("says what the page shows and names the collection, for the system and scope of the page", async () => {
    getSubjects.mockResolvedValue(global());
    renderApp(<Subjects />, "/subjects?system=example-global&account=APP");
    expect(await screen.findByText(/^Where messages can go: stream subjects and client subscriptions, by subject pattern\. In the collection of/)).toHaveTextContent("12:00:00 UTC");
    expect(getSubjects.mock.calls[0].slice(0, 1)).toEqual(["example-global"]);
    expect(getSubjects.mock.calls[0].slice(2, 4)).toEqual(["APP", "all"]);
  });

  it("says only that the range holds no collection", async () => {
    getSubjects.mockResolvedValue(noCollection());
    renderApp(<Subjects />, "/subjects");
    expect(await screen.findByText("No collection in this range.")).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Subject patterns" })).toBeNull();
  });

  it("lists the rows with the board's columns, words and links", async () => {
    getSubjects.mockResolvedValue(global());
    renderApp(<Subjects />, "/subjects?system=example-global");
    await screen.findByRole("region", { name: "Subject patterns" });
    expect(table().getAllByRole("columnheader").map((h) => h.textContent)).toEqual(["Pattern", "Account", "Stored in", "Subscribers", "Stored msgs/s", "Note"]);
    expect(cells("order.events.>")).toEqual(["APP", "order-events", "4 durable consumers", "2.88", "mirrored by order-archive"]);
    expect(cells("$KV.feature-flags.>")).toEqual(["APP", "KV_feature-flags", "22 watchers (api-gateway, checkout-service, order-service +1)", "0.04", ""]);
    expect(cells("orders.place")).toEqual(["APP", "", "3 connections in queue group order-service (order-service)", "– no stored rate", ""]);
    expect(cells("$JS.API.>")).toEqual(["APP", "", "server", "– no stored rate", "JetStream API: 3.1 req/s, 0.2% errors"]);
    expect(cells(">")).toEqual(["SYSsystem", "", "1 connection (ops-monitor)", "– no stored rate", "receives every message in the account"]);

    const r = within(rowOf("order.events.>"));
    expect(r.getByRole("link", { name: "order-events" })).toHaveAttribute("href", "/streams/APP/order-events?system=example-global");
    expect(r.getByRole("link", { name: "4 durable consumers" })).toHaveAttribute("href", "/consumers?stream=APP%2Forder-events&system=example-global");
    expect(r.getByRole("link", { name: "order-archive" })).toHaveAttribute("href", "/streams/APP/order-archive?system=example-global");
    expect(r.getByRole("link", { name: "APP" })).toHaveAttribute("href", "/subjects?account=APP&system=example-global");
    expect(within(rowOf("stock.alerts.>")).getByRole("link", { name: "1 ephemeral" })).toHaveAttribute("href", "/consumers?stream=APP%2Fstock-alerts&ephemeral=1&system=example-global");
    expect(within(rowOf("orders.place")).getByRole("link", { name: "order-service" })).toHaveAttribute("href", "/connections?name=order-service&system=example-global");
  });

  it("shows a mirror without a pattern and a stream without a consumer as a warning row", async () => {
    getSubjects.mockResolvedValue(global());
    renderApp(<Subjects />, "/subjects");
    await screen.findByRole("region", { name: "Subject patterns" });
    const mirror = table().getByRole("rowheader", { name: "no subjects of its own" }).closest("tr") as HTMLElement;
    expect(within(mirror).getAllByRole("cell").map((c) => c.textContent)).toEqual(["APP", "order-archive", "1 durable consumer", "14.9", "mirror of order-events"]);
    expect(rowOf("basket.events.>")).toHaveClass("sj-warn");
    expect(cells("basket.events.>")[4]).toBe("stream without a consumer: messages are kept but nobody reads them");
    expect(cells("basket.events.>")[2]).toBe("none");
    expect(rowOf("order.events.>")).not.toHaveClass("sj-warn");
  });

  it("offers an account as a link to the page scoped to it only when the page is not scoped", async () => {
    getSubjects.mockResolvedValue(global());
    renderApp(<Subjects />, "/subjects?account=APP");
    await screen.findByRole("region", { name: "Subject patterns" });
    expect(within(rowOf("order.events.>")).queryByRole("link", { name: "APP" })).toBeNull();
  });

  it("shows each filter's count and asks for the chosen filter's rows", async () => {
    getSubjects.mockResolvedValue(global());
    renderApp(<Subjects />, "/subjects");
    const all = await screen.findByRole("button", { name: "All patterns 9" });
    expect(all).toHaveAttribute("aria-pressed", "true");
    for (const name of ["Captured by a stream 5", "Stream without a consumer 1", "Overlapping streams 0", "Wide subscriptions (>) 1"]) {
      expect(screen.getByRole("button", { name })).toHaveAttribute("aria-pressed", "false");
    }
    await userEvent.click(screen.getByRole("button", { name: "Stream without a consumer 1" }));
    await waitFor(() => expect(getSubjects).toHaveBeenLastCalledWith("", expect.anything(), "", "noConsumer", expect.anything()));
    expect(location()).toBe("/subjects?filter=no-consumer");
    expect(screen.getByRole("button", { name: "Stream without a consumer 1" })).toHaveAttribute("aria-pressed", "true");
  });

  it("says no row matches the filter by its name", async () => {
    getSubjects.mockResolvedValue({ ...global(), rows: [], rowsTotal: 0 });
    renderApp(<Subjects />, "/subjects?filter=overlapping");
    expect(await screen.findByText("No pattern matches “Overlapping streams” in this collection.")).toBeInTheDocument();
  });

  it("says when the list was cut and what the footnote knows of the collection", async () => {
    getSubjects.mockResolvedValue({ ...global(), rowsTotal: 1204 });
    renderApp(<Subjects />, "/subjects");
    expect(await screen.findByText("The first 9 of 1,204 rows are listed.")).toBeInTheDocument();
    expect(screen.getByText(/^NATS servers do not count messages per subject/)).toBeInTheDocument();
    expect(screen.getByText("Subscriptions as of 11:58:30 UTC.")).toBeInTheDocument();
    expect(screen.getByText("nats-2 did not answer for its subscription list; its subscriptions are missing.")).toBeInTheDocument();
    expect(screen.getByText("Lists cut at the limit: nats-1 listed 10,000 of 14,212.")).toBeInTheDocument();
    expect(screen.getByText(/^1,006 subscriptions of the servers' own clients/)).toBeInTheDocument();
  });

  it("keeps the filter on an account's link", async () => {
    getSubjects.mockResolvedValue(global());
    renderApp(<Subjects />, "/subjects?filter=stored");
    await screen.findByRole("region", { name: "Subject patterns" });
    expect(within(rowOf("order.events.>")).getByRole("link", { name: "APP" })).toHaveAttribute("href", "/subjects?account=APP&filter=stored");
  });

  it("keeps a queue group's name on one line", async () => {
    getSubjects.mockResolvedValue(global());
    renderApp(<Subjects />, "/subjects");
    await screen.findByRole("region", { name: "Subject patterns" });
    expect(within(rowOf("orders.place")).getByText("order-service", { selector: ".sj-name" })).toBeInTheDocument();
  });

  it("keeps each connection name with its comma in a box of its own", async () => {
    getSubjects.mockResolvedValue(global());
    renderApp(<Subjects />, "/subjects");
    await screen.findByRole("region", { name: "Subject patterns" });
    const names = [...rowOf("$KV.feature-flags.>").querySelectorAll(".sj-names .sj-name")].map((n) => n.textContent);
    expect(names).toEqual(["(api-gateway,", "checkout-service,", "order-service"]);
    expect(within(rowOf("orders.place")).getByText((_, el) => el?.className === "sj-name" && el.textContent === "(order-service)")).toBeInTheDocument();
  });

  it("renders a pattern as plain text, so it reads as one subject", async () => {
    getSubjects.mockResolvedValue(global());
    renderApp(<Subjects />, "/subjects");
    await screen.findByRole("region", { name: "Subject patterns" });
    const p = rowOf("$KV.feature-flags.>").querySelector(".sj-pattern")!;
    expect(p.children).toHaveLength(0);
    expect(p.textContent).toBe("$KV.feature-flags.>");
  });

  it("shows no counts on the pills when the range holds no collection", async () => {
    getSubjects.mockResolvedValue(noCollection());
    renderApp(<Subjects />, "/subjects");
    await screen.findByText("No collection in this range.");
    expect(screen.getByRole("button", { name: "All patterns" })).toHaveTextContent(/^All patterns$/);
    expect(screen.queryByRole("button", { name: "All patterns 0" })).toBeNull();
  });

  it("says subscriptions were not collected, and still lists the streams", async () => {
    getSubjects.mockResolvedValue({ ...global(), subscriptionsAt: undefined, servers: [], rows: [row({ durable: 2 })] });
    renderApp(<Subjects />, "/subjects");
    expect(await screen.findByRole("note")).toHaveTextContent(/^Subscriptions not collected\./);
    expect(cells("order.events.>")[2]).toBe("2 durable consumers");
    expect(screen.queryByText(/^Subscriptions as of/)).toBeNull();
    // Once, in the notice: not again in the footnote, which leaves out how subscription lists are read.
    const page = document.body.textContent ?? "";
    expect(page.match(/not collected/gi)).toHaveLength(1);
    expect(page).not.toMatch(/refreshed every few minutes/);
  });
});

describe("Subjects tester", () => {
  it("asks once the field is still and shows what the subject reaches, by the account it is published in", async () => {
    getSubjects.mockResolvedValue(global());
    matchSubject.mockImplementation(async (_s, _r, _a, subject) =>
      subject === "order.events.created"
        ? matches({
            accounts: [
              published({ streams: [streamMatch()] }),
              published({ account: "SHOP", streams: [streamMatch({ account: "SHOP", stream: "shop-orders", consumers: [], mirrors: [] })], core: [coreMatch({ queue: "", subject: ">", connections: 1, names: ["ops-monitor"] })] }),
            ],
          })
        : matches(),
    );
    renderApp(<Subjects />, "/subjects?system=example-global");
    await userEvent.type(field(), "order.events.created");
    const card = within(await screen.findByRole("region", { name: "Where this subject goes" }));
    await card.findByRole("group", { name: "Published in APP" });
    expect(card.getByText("order.events.created", { selector: ".sj-subject" })).toBeInTheDocument();
    const app = within(card.getByRole("group", { name: "Published in APP" }));
    expect(app.getByText("Published in")).toBeInTheDocument();
    expect(app.getByRole("link", { name: "order-events" })).toHaveAttribute("href", "/streams/APP/order-events?system=example-global");
    expect(app.getByRole("link", { name: "email-sender" })).toHaveAttribute("href", "/consumers/APP/order-events/email-sender?system=example-global");
    expect(app.getByRole("link", { name: "order-archive" })).toBeInTheDocument();
    expect(app.getByText(/^Core subscribers/).parentElement).toHaveTextContent("Core subscribers none");
    const shop = within(card.getByRole("group", { name: "Published in SHOP" }));
    expect(shop.getByText("no consumer reads it")).toBeInTheDocument();
    expect(shop.getByText(/^Core subscribers/).parentElement).toHaveTextContent("Core subscribers 1 connection (ops-monitor) via >");
    // The first letter is asked at once, then only the whole subject.
    const asked = matchSubject.mock.calls.map((c) => c[3]);
    expect(asked[asked.length - 1]).toBe("order.events.created");
    expect(asked.length).toBeLessThanOrEqual(3);
    expect(matchSubject.mock.calls[0][0]).toBe("example-global");
  });

  it("marks the system account and says when the account's JetStream API answers", async () => {
    getSubjects.mockResolvedValue(global());
    matchSubject.mockResolvedValue(
      matches({ accounts: [published({ jetstreamApi: true }), published({ account: "SYS", systemAccount: true, core: [coreMatch({ queue: "", subject: "$SYS.>" })] })] }),
    );
    renderApp(<Subjects />, "/subjects");
    await userEvent.type(field(), "x");
    const card = within(await screen.findByRole("region", { name: "Where this subject goes" }));
    const app = within(await card.findByRole("group", { name: "Published in APP" }));
    expect(app.getByText("the account's JetStream API answers it")).toBeInTheDocument();
    expect(within(card.getByRole("group", { name: "Published in SYS" })).getByText("system")).toBeInTheDocument();
  });

  it("leaves the account heading out when the page shows one account, and asks in it", async () => {
    getSubjects.mockResolvedValue(global());
    matchSubject.mockResolvedValue(matches({ accounts: [published({ core: [coreMatch()] })] }));
    renderApp(<Subjects />, "/subjects?account=APP");
    await userEvent.type(field(), "x");
    const card = within(await screen.findByRole("region", { name: "Where this subject goes" }));
    expect(await card.findByText((_, el) => el?.classList.contains("sj-chip") === true && /one of 3 in queue group order-service/.test(el.textContent ?? ""))).toBeInTheDocument();
    expect(card.queryByText("Published in")).toBeNull();
    expect(matchSubject.mock.calls[0][2]).toBe("APP");
  });

  it("says the scoped account's empty group reaches nothing", async () => {
    getSubjects.mockResolvedValue(global());
    matchSubject.mockResolvedValue(matches({ accounts: [published()] }));
    renderApp(<Subjects />, "/subjects?account=APP");
    await userEvent.type(field(), "x");
    const card = within(await screen.findByRole("region", { name: "Where this subject goes" }));
    expect(await within(card.getByRole("group", { name: "Published in APP" })).findByText("no stream stores it and no subscriber listens")).toBeInTheDocument();
  });

  it("says when nothing reaches the subject in any account, and when subscribers are not known", async () => {
    getSubjects.mockResolvedValue(global());
    matchSubject.mockResolvedValueOnce(matches());
    renderApp(<Subjects />, "/subjects");
    await userEvent.type(field(), "x");
    expect(await screen.findByText("no stream stores it and no subscriber listens")).toBeInTheDocument();
    matchSubject.mockResolvedValueOnce(matches({ subscriptionsAt: undefined }));
    await userEvent.type(field(), "y");
    expect(await screen.findByText("no stream stores it; subscriptions were not collected, so subscribers are not known")).toBeInTheDocument();
  });

  it("shows the server's reason in the field for a subject it refuses, and no card", async () => {
    getSubjects.mockResolvedValue(global());
    matchSubject.mockRejectedValue(new ConnectError('invalid argument: subject "order.*" has a wildcard; give a literal subject', Code.InvalidArgument));
    renderApp(<Subjects />, "/subjects");
    await userEvent.type(field(), "order.*");
    expect(await screen.findByText('subject "order.*" has a wildcard; give a literal subject')).toHaveAttribute("aria-live", "polite");
    expect(screen.queryByRole("alert")).toBeNull();
    expect(field()).toHaveAttribute("aria-invalid", "true");
    expect(field()).toHaveAccessibleDescription('subject "order.*" has a wildcard; give a literal subject');
    expect(screen.queryByRole("region", { name: "Where this subject goes" })).toBeNull();
  });

  it("never shows the answer of a subject typed before", async () => {
    getSubjects.mockResolvedValue(global());
    let release: (m: SubjectMatches) => void = () => {};
    matchSubject.mockImplementation(async (_s, _r, _a, subject) =>
      subject === "a" ? matches({ accounts: [published({ streams: [streamMatch({ stream: "first-answer" })] })] }) : new Promise<SubjectMatches>((r) => (release = r)),
    );
    renderApp(<Subjects />, "/subjects");
    await userEvent.type(field(), "a");
    const card = within(await screen.findByRole("region", { name: "Where this subject goes" }));
    expect(await card.findByRole("link", { name: "first-answer" })).toBeInTheDocument();
    await userEvent.type(field(), "b");
    expect(card.queryByRole("link", { name: "first-answer" })).toBeNull();
    expect(card.getByText("Looking it up…")).toBeInTheDocument();
    await waitFor(() => expect(matchSubject.mock.calls.map((c) => c[3])).toContain("ab"));
    await act(async () => release(matches({ accounts: [published({ streams: [streamMatch({ stream: "second-answer" })] })] })));
    expect(await card.findByRole("link", { name: "second-answer" })).toBeInTheDocument();
  });

  it("words a KV bucket's watchers and does not warn when a bucket, an object store or a copied stream has no reader", async () => {
    getSubjects.mockResolvedValue(global());
    matchSubject.mockResolvedValue(
      matches({
        accounts: [
          published({
            streams: [
              streamMatch({ stream: "KV_flags", kind: "kv", consumers: [], ephemeral: 22, mirrors: [] }),
              streamMatch({ stream: "KV_cache", kind: "kv", consumers: [], ephemeral: 0, mirrors: [] }),
              streamMatch({ stream: "OBJ_images", kind: "object", consumers: [], ephemeral: 0, mirrors: [] }),
              streamMatch({ stream: "order-events", consumers: [], ephemeral: 0, mirrors: ["order-archive"] }),
              streamMatch({ stream: "basket-events", consumers: [], ephemeral: 0, mirrors: [], sourcedBy: [] }),
            ],
          }),
        ],
      }),
    );
    const { container } = renderApp(<Subjects />, "/subjects");
    await userEvent.type(field(), "x");
    const card = within(await screen.findByRole("region", { name: "Where this subject goes" }));
    await card.findByRole("link", { name: "KV_flags" });
    const chips = [...container.querySelectorAll(".sj-reach .sj-chip:not(.sj-chip-stream)")].map((c) => [c.textContent, c.classList.contains("sj-chip-warn")]);
    expect(chips).toEqual([
      ["then read by 22 watchers", false],
      ["no watcher", false],
      ["no consumer", false],
      ["no consumer", false],
      ["Mirror order-archive", false],
      ["no consumer reads it", true],
      ["Core subscribers none", false],
    ]);
  });

  it("announces each answer through one live region that stays while the card does", async () => {
    getSubjects.mockResolvedValue(global());
    let release: (m: SubjectMatches) => void = () => {};
    matchSubject.mockImplementation(() => new Promise<SubjectMatches>((r) => (release = r)));
    const { container } = renderApp(<Subjects />, "/subjects");
    await userEvent.type(field(), "x");
    await screen.findByText("Looking it up…");
    const live = container.querySelector(".sj-reach [aria-live=polite]");
    expect(live).toHaveTextContent("Looking it up…");
    await act(async () => release(matches()));
    await screen.findByText("no stream stores it and no subscriber listens");
    expect(container.querySelector(".sj-reach [aria-live=polite]")).toBe(live);
    expect(within(container.querySelector(".sj-reach")! as HTMLElement).queryByRole("status")).toBeNull();
  });

  it("says once that the range holds no collection when a subject is typed", async () => {
    getSubjects.mockResolvedValue(noCollection());
    matchSubject.mockResolvedValue({ collected: false, accounts: [] });
    renderApp(<Subjects />, "/subjects");
    await screen.findByText("No collection in this range.");
    await userEvent.type(field(), "x");
    await waitFor(() => expect(matchSubject).toHaveBeenCalled());
    await act(async () => {});
    expect(screen.getAllByText("No collection in this range.")).toHaveLength(1);
  });

  it("keeps a core match's queue group on one line", async () => {
    getSubjects.mockResolvedValue(global());
    matchSubject.mockResolvedValue(matches({ accounts: [published({ core: [coreMatch()] })] }));
    renderApp(<Subjects />, "/subjects");
    await userEvent.type(field(), "x");
    const card = within(await screen.findByRole("region", { name: "Where this subject goes" }));
    expect(await card.findByText("order-service", { selector: ".sj-name" })).toBeInTheDocument();
  });

  it("asks nothing for an empty or blank field", async () => {
    getSubjects.mockResolvedValue(global());
    renderApp(<Subjects />, "/subjects");
    await screen.findByRole("region", { name: "Subject patterns" });
    await userEvent.type(field(), "   ");
    expect(matchSubject).not.toHaveBeenCalled();
    expect(screen.queryByRole("region", { name: "Where this subject goes" })).toBeNull();
  });
});

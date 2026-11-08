import { Remediation } from "./Remediation";
import { Code, ConnectError } from "@connectrpc/connect";
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ErrorBox, QueryView } from "./common";

const at = Date.parse("2026-10-17T14:30:05Z");

describe("QueryView", () => {
  it("keeps showing data after a failed refresh, under a banner that says so", () => {
    render(
      <QueryView q={{ data: 1, error: new ConnectError("refused", Code.Unavailable), isPending: false, refetch: () => {}, dataUpdatedAt: at }} what="streams">
        {(d) => <i>data {d}</i>}
      </QueryView>,
    );
    expect(screen.getByText("data 1")).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Showing data from 14:30:05 UTC; the last refresh failed: unavailable.");
  });

  it("shows no banner while refreshes succeed", () => {
    render(<QueryView q={{ data: 1, error: null, isPending: false, refetch: () => {}, dataUpdatedAt: at }} what="w">{(d) => <i>data {d}</i>}</QueryView>);
    expect(screen.queryByRole("status")).toBeNull();
  });
});

describe("ErrorBox", () => {
  it("shows the code, the message and a hint", () => {
    render(<ErrorBox error={new ConnectError("refused", Code.Unavailable)} what="servers" />);
    expect(screen.getByRole("alert")).toHaveTextContent("Could not load servers. unavailable refused The nats-lens server cannot be reached");
  });
});

describe("one stale banner per page", () => {
  it("collects every failing query into one banner with the newest time and one Retry", async () => {
    const { StaleProvider } = await import("./StaleContext");
    const { default: userEvent } = await import("@testing-library/user-event");
    const r1 = vi.fn();
    const r2 = vi.fn();
    const err = new ConnectError("refused", Code.Unavailable);
    render(
      <StaleProvider>
        <QueryView q={{ data: 1, error: err, isPending: false, refetch: r1, dataUpdatedAt: at }} what="a">{() => <i>a</i>}</QueryView>
        <QueryView q={{ data: 2, error: err, isPending: false, refetch: r2, dataUpdatedAt: at + 60_000 }} what="b">{() => <i>b</i>}</QueryView>
        <QueryView q={{ data: 3, error: null, isPending: false, refetch: vi.fn(), dataUpdatedAt: at }} what="c">{() => <i>c</i>}</QueryView>
      </StaleProvider>,
    );
    const banners = await screen.findAllByRole("status");
    expect(banners).toHaveLength(1);
    expect(banners[0]).toHaveTextContent("Showing data from 14:31:05 UTC; the last refresh failed: unavailable.");
    await userEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(r1).toHaveBeenCalledTimes(1);
    expect(r2).toHaveBeenCalledTimes(1);
  });
});

describe("Dash", () => {
  it("shows a dash and reads as not observed through visually hidden text", async () => {
    const { Dash, Num } = await import("./common");
    const { container } = render(
      <p>
        <Dash /> <Num v={undefined} /> <Num v={1240} />
      </p>,
    );
    expect(screen.getAllByText("not observed")[0]).toHaveClass("sr-only");
    expect(container.querySelector("[aria-label]")).toBeNull();
    expect(container.querySelectorAll('[aria-hidden="true"]')).toHaveLength(2);
    expect(container.textContent).toContain("1,240");
  });
});

describe("Dash copied as text", () => {
  it("copies as a dash, a space and not observed", async () => {
    const { Dash } = await import("./common");
    const { container } = render(<Dash />);
    expect(container.textContent).toBe("– not observed");
  });
});


describe("Markdown guidance", () => {
  it("renders tables on shared guidance surfaces with inline formatting and escaped pipes", () => {
    render(<Remediation text={'Before.\n\n| Setting | Meaning |\n| :--- | ---: |\n| `max_waiting` | **Full** and *waiting*; [guide](https://example.com/guide) |\n| `a\\|b` | literal a\\|b |\n\nAfter.'} />);
    const table = screen.getByRole("table");
    expect(table.querySelector("thead th")).toHaveAttribute("scope", "col");
    expect(table.querySelectorAll("tbody td")).toHaveLength(4);
    expect(within(table).getByText("max_waiting").tagName).toBe("CODE");
    expect(within(table).getByText("Full").tagName).toBe("STRONG");
    expect(within(table).getByText("waiting").tagName).toBe("EM");
    expect(within(table).getByRole("link", {name: "guide"})).toHaveAttribute("href", "https://example.com/guide");
    expect(within(table).getByText("a|b").tagName).toBe("CODE");
    expect(within(table).getByText("literal a|b")).toBeInTheDocument();
    expect(table.parentElement).toHaveAttribute("tabindex", "0");
    expect(table.parentElement).toHaveAttribute("aria-label", "Scrollable table");
    expect(screen.getByText("Before.")).toBeInTheDocument();
    expect(screen.getByText("After.")).toBeInTheDocument();
  });

  it("preserves headings, lists, code blocks, links and literal non-table pipes", () => {
    const { container } = render(<Remediation text={'### Inspect next\n\n- **First**, inspect `orders`.\n- Then [read the guide](/checks/stream-001).\n\n```sh\necho "a|b"\n```\n\nA | B is prose.\n\n| malformed | row |\n| no separator | here |'} />);
    expect(screen.getByRole("heading", {name: "Inspect next", level: 3})).toBeInTheDocument();
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    expect(screen.getByRole("link", {name: "read the guide"})).toHaveAttribute("href", "/checks/stream-001");
    expect(container.querySelector("pre code")).toHaveTextContent('echo "a|b"');
    expect(screen.queryByRole("table")).toBeNull();
    expect(container).toHaveTextContent("A | B is prose.");
    expect(container).toHaveTextContent("| no separator | here |");
  });

  it("keeps raw HTML inert and blocks executable links inside and outside tables", () => {
    const { container } = render(<Remediation text={'<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>\n\n[script](javascript:alert%281%29) [data](data:text/html,boom) [control](java&#x09;script:alert%281%29)\n\n| Link | Value |\n| --- | --- |\n| [vb](vbscript:boom) | [safe](https://example.com) |\n| [encoded](javascript&#x3a;boom) | <img src=x onerror=alert(1)> |'} />);
    expect(container.querySelector("script, img, iframe, [onerror]")).toBeNull();
    for (const name of ["script", "data", "control", "vb", "encoded"]) {
      const anchor = screen.getByText(name).closest("a");
      expect(anchor?.getAttribute("href") || "").toBe("");
    }
    expect(screen.getByRole("link", {name:"safe"})).toHaveAttribute("href", "https://example.com");
  });
});

import { act, fireEvent, screen } from "@testing-library/react";
import { useNavigate } from "react-router";
import { beforeEach, expect, it, vi } from "vitest";
import { App } from "./App";
import { lens } from "./api/client";
import { renderApp } from "./test/render";

const state = vi.hoisted(() => ({ failure: false, pending: false, resolve: () => {}, promise: Promise.resolve() }));
vi.mock("./pages/Checks", () => ({
  Checks: () => {
    if (state.failure) throw new TypeError("Failed to fetch dynamically imported module");
    if (state.pending) throw state.promise;
    return <h2>Check catalogue ready</h2>;
  },
  CheckDetail: () => <h1>Check reference ready</h1>,
}));
vi.mock("./api/requests", async orig => ({ ...await orig<typeof import("./api/requests")>(), getLensInfo: vi.fn(() => new Promise(() => {})) }));
vi.mock("./components/SystemGate", () => ({ SystemGate: ({ children }: { children: React.ReactNode }) => children, Landing: ({ children }: { children: React.ReactNode }) => children }));
vi.mock("./components/Header", () => ({ Header: () => <header><h1>Checks</h1></header> }));
vi.mock("./components/Sidebar", () => ({ Sidebar: () => <aside>Page navigation</aside> }));
vi.mock("./api/client", () => ({ lens: { listCheckDefinitions: vi.fn(() => new Promise(() => {})) } }));
let go: ReturnType<typeof useNavigate>;
function Navigate() { go = useNavigate(); return null; }
beforeEach(() => {
  state.failure = false; state.pending = false;
  state.promise = new Promise<void>(resolve => { state.resolve = resolve; });
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
  vi.spyOn(console, "error").mockImplementation(() => {});
});
it("keeps the shell and announces loading while route code is pending", async () => {
  state.pending = true;
  renderApp(<App />, "/checks?system=shop&range=6h&account=APP");
  expect((await screen.findByText("Loading page…")).closest('[role="status"]')).toHaveAttribute("aria-live", "polite");
  expect(screen.getByText("Page navigation")).toBeInTheDocument();
  expect(lens.listCheckDefinitions).toHaveBeenCalledWith({ system: "shop" });
  state.pending = false;
  await act(async () => state.resolve());
  expect(await screen.findByText("Check catalogue ready")).toBeInTheDocument();
  expect(screen.getByTestId("location")).toHaveTextContent("/checks?system=shop&range=6h&account=APP");
});
it("contains failed route code and offers a full-page reload without losing the URL", async () => {
  state.failure = true;
  renderApp(<><App /><Navigate /></>, "/checks?system=shop&range=6h&account=APP");
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not load this page");
  expect(screen.getByText("Page navigation")).toBeInTheDocument();
  const reload = screen.getByRole("link", { name: "Reload page" });
  expect(reload).toHaveAttribute("href", "/checks?system=shop&range=6h&account=APP");
  // A different route resets the error; the persistent shell remains usable.
  state.failure = false;
  act(() => go("/checks/server-014?system=shop&range=6h"));
  expect(await screen.findByRole("heading", { name: "Check reference ready" })).toBeInTheDocument();
  expect(screen.queryByRole("alert")).toBeNull();
  fireEvent.click(screen.getByRole("heading", { name: "Check reference ready" }));
});

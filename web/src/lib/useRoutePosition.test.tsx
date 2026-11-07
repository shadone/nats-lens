import { act, render, screen, waitFor } from "@testing-library/react";
import { StrictMode, useState } from "react";
import { MemoryRouter, useLocation, useNavigate } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useRoutePosition } from "./useRoutePosition";

let go: ReturnType<typeof useNavigate>;
let ready: (value: boolean) => void;
function Page() {
  go = useNavigate();
  const location = useLocation();
  const [loaded, setLoaded] = useState(true);
  ready = setLoaded;
  const main = useRoutePosition();
  return <div><h1>Section</h1><main ref={main} tabIndex={-1} id="main">
    {loaded && <><h1>{location.pathname}</h1><h2 id="evidence">Evidence</h2></>}
  </main><button>Range</button></div>;
}
let y = 0;
let height = 5000;
const scroll = vi.fn((_x: number, top: number) => { y = Math.min(top, height - 800); });
beforeEach(() => {
  y = 0; height = 5000;
  vi.stubGlobal("scrollTo", scroll.mockClear());
  vi.spyOn(window, "scrollY", "get").mockImplementation(() => y);
  vi.spyOn(document.documentElement, "scrollHeight", "get").mockImplementation(() => height);
  vi.spyOn(window, "innerHeight", "get").mockReturnValue(800);
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  HTMLElement.prototype.scrollIntoView = vi.fn();
});
afterEach(() => vi.unstubAllGlobals());
function mount(path = "/checks") { return render(<MemoryRouter initialEntries={[path]}><Page /></MemoryRouter>); }
function readAt(top: number) { act(() => { y = top; window.dispatchEvent(new Event("scroll")); }); }

it("starts a new path at the top and focuses its heading, including cached content", async () => {
  mount(); readAt(1200);
  act(() => go("/checks/server014?system=shop&range=6h"));
  expect(scroll).toHaveBeenLastCalledWith(0, 0);
  await waitFor(() => expect(screen.getByRole("heading", { name: "/checks/server014" })).toHaveFocus());
});
it("preserves reading position and focus for all query-only navigation", () => {
  mount(); readAt(900); screen.getByRole("button").focus();
  act(() => go("/checks?range=6h&system=shop&account=APP"));
  expect(y).toBe(900); expect(scroll).not.toHaveBeenCalled();
  expect(screen.getByRole("button")).toHaveFocus();
});
it("restores each history entry on Back and Forward, including query entries", async () => {
  mount(); readAt(1300);
  act(() => go("/checks?range=6h")); readAt(1600);
  act(() => go("/checks/server014")); readAt(300);
  act(() => go(-1)); expect(y).toBe(1600);
  act(() => go(-1)); expect(y).toBe(1300);
  act(() => go(1)); expect(y).toBe(1600);
  act(() => go(1)); expect(y).toBe(300);
});
it("waits for asynchronous content before restoring a catalog position", async () => {
  mount(); readAt(2400);
  act(() => go("/checks/server014"));
  act(() => { ready(false); }); height = 900;
  act(() => go(-1)); expect(y).toBe(100);
  height = 5000;
  act(() => ready(true));
  await waitFor(() => expect(y).toBe(2400));
});
it("focuses a late detail heading without scrolling again", async () => {
  mount(); act(() => ready(false)); readAt(800);
  act(() => go("/servers/nats-1")); expect(screen.getByRole("main")).toHaveFocus();
  act(() => ready(true));
  await waitFor(() => expect(screen.getByRole("heading", { name: "/servers/nats-1" })).toHaveFocus());
  expect(y).toBe(0);
});
it("honors explicit anchors, including on initial load and malformed fragments", async () => {
  mount("/checks#evidence");
  expect(screen.getByText("Evidence").scrollIntoView).toHaveBeenCalled();
  expect(screen.getByText("Evidence")).toHaveFocus();
  act(() => go("/checks#%ZZ"));
  expect(screen.getByRole("main")).toBeInTheDocument();
});
it("cancels pending restoration when the reader takes control", async () => {
  mount(); readAt(2400); act(() => go("/checks/server014"));
  act(() => ready(false)); height = 900; act(() => go(-1));
  act(() => window.dispatchEvent(new Event("wheel")));
  height = 5000; readAt(50); act(() => ready(true));
  await act(async () => {}); expect(y).toBe(50);
});
it("leaves initial focus and scroll alone without an anchor", () => {
  y = 500; mount(); expect(scroll).not.toHaveBeenCalled(); expect(document.body).toHaveFocus();
});

it("does not reset a Strict Mode initial load", () => {
  y = 500;
  render(<StrictMode><MemoryRouter initialEntries={["/checks"]}><Page /></MemoryRouter></StrictMode>);
  expect(y).toBe(500); expect(scroll).not.toHaveBeenCalled(); expect(document.body).toHaveFocus();
});
it("does not steal focus from a reader while a detail heading loads", async () => {
  mount(); act(() => ready(false)); act(() => go("/checks/server014"));
  act(() => window.dispatchEvent(new Event("pointerdown")));
  screen.getByRole("button").focus(); act(() => ready(true));
  await act(async () => {}); expect(screen.getByRole("button")).toHaveFocus();
});
it("honors an anchor that arrives asynchronously", async () => {
  mount(); act(() => ready(false)); act(() => go("/checks/server014#evidence"));
  act(() => ready(true));
  await waitFor(() => expect(screen.getByText("Evidence")).toHaveFocus());
});

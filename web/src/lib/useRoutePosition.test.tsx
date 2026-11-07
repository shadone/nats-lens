import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { StrictMode, useState } from "react";
import { Link, MemoryRouter, useLocation, useNavigate } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useRoutePosition } from "./useRoutePosition";
import { useSidebar } from "./useSidebar";

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

it("review: restores saved position rather than reactivating a fragment on POP", () => {
  mount("/checks#evidence");
  readAt(2100);
  act(() => go("/servers"));
  readAt(100);
  vi.mocked(HTMLElement.prototype.scrollIntoView).mockClear();
  act(() => go(-1));
  expect(y).toBe(2100);
  expect(HTMLElement.prototype.scrollIntoView).not.toHaveBeenCalled();
});


it("restores fragment entries on same-path Back and Forward without activating their anchors", () => {
  mount("/checks#evidence"); readAt(2100);
  act(() => go("/checks?range=6h#evidence")); readAt(1700);
  act(() => go("/checks#other")); readAt(800);
  vi.mocked(HTMLElement.prototype.scrollIntoView).mockClear();
  act(() => go(-1)); expect(y).toBe(1700);
  act(() => go(-1)); expect(y).toBe(2100);
  act(() => go(1)); expect(y).toBe(1700);
  expect(HTMLElement.prototype.scrollIntoView).not.toHaveBeenCalled();
});
it("waits for cold content when restoring a saved fragment entry", async () => {
  mount("/checks#evidence"); readAt(2100);
  act(() => go("/servers")); act(() => ready(false)); height = 900;
  vi.mocked(HTMLElement.prototype.scrollIntoView).mockClear();
  act(() => go(-1)); expect(y).toBe(100);
  height = 5000; act(() => ready(true));
  await waitFor(() => expect(y).toBe(2100));
  expect(HTMLElement.prototype.scrollIntoView).not.toHaveBeenCalled();
  expect(screen.getByRole("heading", { name: "/checks" })).toHaveFocus();
});
it("still activates a fragment on a new same-path navigation", () => {
  mount(); readAt(2100);
  act(() => go("/checks#evidence"));
  expect(HTMLElement.prototype.scrollIntoView).toHaveBeenCalledOnce();
  expect(screen.getByText("Evidence")).toHaveFocus();
});
it("stops waiting for a saved fragment position after ten seconds", async () => {
  vi.useFakeTimers();
  try {
    mount("/checks#evidence"); readAt(2100); act(() => go("/servers"));
    act(() => ready(false)); height = 900; act(() => go(-1));
    act(() => vi.advanceTimersByTime(10_000));
    height = 5000; readAt(50); act(() => ready(true));
    await act(async () => {}); expect(y).toBe(50);
  } finally { vi.useRealTimers(); }
});
it("bounds saved positions to the most recent 100 entries", () => {
  mount(); readAt(2100);
  for (let i = 0; i < 101; i++) act(() => go(`/checks?entry=${i}`));
  readAt(100);
  act(() => go(-101));
  expect(y).toBe(100);
});

it("R2: reactivates the same explicit fragment on a new PUSH", () => {
  mount("/checks#evidence"); readAt(2100);
  vi.mocked(HTMLElement.prototype.scrollIntoView).mockClear();
  act(() => go("/checks#evidence"));
  expect(HTMLElement.prototype.scrollIntoView).toHaveBeenCalledOnce();
  expect(screen.getByText("Evidence")).toHaveFocus();
});
it("R2: REPLACE query retains position and path REPLACE starts at top", () => {
  mount(); readAt(1800); screen.getByRole("button").focus();
  act(() => go("/checks?range=6h", { replace: true }));
  expect(y).toBe(1800); expect(screen.getByRole("button")).toHaveFocus();
  act(() => go("/servers/a", { replace: true })); expect(y).toBe(0);
  expect(screen.getByRole("heading", {name:"/servers/a"})).toHaveFocus();
});
it("R2: unsaved POP entry honors fragment", () => {
  render(<MemoryRouter initialEntries={["/checks#evidence", "/servers"]} initialIndex={1}><Page /></MemoryRouter>);
  readAt(900); act(() => go(-1));
  expect(HTMLElement.prototype.scrollIntoView).toHaveBeenCalledOnce();
  expect(screen.getByText("Evidence")).toHaveFocus();
});
it.each(["touchstart", "pointerdown", "keydown"])("R2: %s cancels cold POP restoration", async (input) => {
  mount(); readAt(2400); act(() => go("/servers/a"));
  act(() => ready(false)); height=900; act(() => go(-1));
  act(() => window.dispatchEvent(new Event(input)));
  height=5000; readAt(50); act(() => ready(true));
  await act(async () => {}); expect(y).toBe(50);
});

it("R2: unsaved POP path starts at heading", () => {
 render(<MemoryRouter initialEntries={["/checks", "/servers"]} initialIndex={1}><Page /></MemoryRouter>);
 readAt(900); act(() => go(-1)); expect(y).toBe(0);
 expect(screen.getByRole("heading",{name:"/checks"})).toHaveFocus();
});

function DrawerPage() {
  const main = useRoutePosition();
  const side = useSidebar();
  go = useNavigate();
  const location = useLocation();
  const [loaded, setLoaded] = useState(true);
  ready = setLoaded;
  return <div>
    <button aria-controls="sidebar" onClick={() => side.setDrawerOpen(true)}>Menu</button>
    {side.drawerShown && <aside id="sidebar">
      <Link to="/checks#evidence">Drawer anchor</Link>
    </aside>}
    <div inert={side.drawerShown}>
      <header><h1>{location.pathname}</h1></header>
      <main id="main" tabIndex={-1} ref={main}>
        {loaded && <h2 id="evidence">Evidence</h2>}
      </main>
    </div>
  </div>;
}
function mountDrawer(path: string) {
  // jsdom does not enforce inert focus suppression; model the browser here.
  const nativeFocus = HTMLElement.prototype.focus;
  vi.spyOn(HTMLElement.prototype, "focus").mockImplementation(function (options) {
    if (!this.closest("[inert]")) nativeFocus.call(this, options);
  });
  return render(<MemoryRouter initialEntries={[path]}><DrawerPage /></MemoryRouter>);
}
it.each([false, true])("drawer fragment owns focus after closure (delayed: %s)", async (delayed) => {
  mountDrawer("/servers");
  fireEvent.click(screen.getByRole("button", { name: "Menu" }));
  if (delayed) act(() => ready(false));
  fireEvent.click(screen.getByRole("link", { name: "Drawer anchor" }));
  expect(screen.queryByRole("complementary")).not.toBeInTheDocument();
  if (delayed) act(() => ready(true));
  await waitFor(() => expect(screen.getByText("Evidence")).toHaveFocus());
});
it("closes a drawer and reactivates its same-path fragment", async () => {
  mountDrawer("/checks#evidence"); readAt(2100);
  fireEvent.click(screen.getByRole("button", { name: "Menu" }));
  vi.mocked(HTMLElement.prototype.scrollIntoView).mockClear();
  fireEvent.click(screen.getByRole("link", { name: "Drawer anchor" }));
  await waitFor(() => expect(screen.queryByRole("complementary")).not.toBeInTheDocument());
  await waitFor(() => expect(screen.getByText("Evidence")).toHaveFocus());
  expect(HTMLElement.prototype.scrollIntoView).toHaveBeenCalledOnce();
});
it("preserves position and focus when a query changes with an unchanged fragment", () => {
  mount("/checks#evidence"); readAt(2100); screen.getByRole("button").focus();
  vi.mocked(HTMLElement.prototype.scrollIntoView).mockClear();
  act(() => go("/checks?range=6h#evidence"));
  expect(y).toBe(2100); expect(screen.getByRole("button")).toHaveFocus();
  expect(HTMLElement.prototype.scrollIntoView).not.toHaveBeenCalled();
});
it("reactivates an identical fragment on REPLACE", () => {
  mount("/checks#evidence"); readAt(2100); screen.getByRole("button").focus();
  vi.mocked(HTMLElement.prototype.scrollIntoView).mockClear();
  act(() => go("/checks#evidence", { replace: true }));
  expect(HTMLElement.prototype.scrollIntoView).toHaveBeenCalledOnce();
  expect(screen.getByText("Evidence")).toHaveFocus();
});

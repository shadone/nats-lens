// Sidebar state: collapsed to a rail (kept in this browser) and the drawer
// at phone width (open until the page changes, which gives focus to the
// new page, or Escape is pressed, which gives it back to the menu button).
import { useCallback, useEffect, useRef, useState } from "react";
import { useLocation } from "react-router";

export const SIDEBAR_KEY = "nats-lens-sidebar";

/** The width at which the sidebar is a drawer; the same as in sidebar.css. */
export const PHONE_QUERY = "(max-width:760px)";

/** Whether the window is phone width. Without matchMedia (tests) it counts as phone width. */
function usePhoneWidth(): boolean {
  const mql = typeof window !== "undefined" && window.matchMedia ? window.matchMedia(PHONE_QUERY) : undefined;
  const [phone, setPhone] = useState(mql ? mql.matches : true);
  useEffect(() => {
    if (!mql) return;
    const onChange = (e: { matches: boolean }) => setPhone(e.matches);
    mql.addEventListener("change", onChange);
    return () => mql.removeEventListener("change", onChange);
    // One listener for the life of the shell.
  }, []);
  return phone;
}

function storedCollapsed(): boolean {
  try {
    return localStorage.getItem(SIDEBAR_KEY) === "collapsed";
  } catch {
    return false;
  }
}

export function useSidebar() {
  const [collapsed, setCollapsedState] = useState(storedCollapsed);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const { pathname, search } = useLocation();
  const phone = usePhoneWidth();

  const setCollapsed = useCallback((c: boolean) => {
    setCollapsedState(c);
    try {
      if (c) localStorage.setItem(SIDEBAR_KEY, "collapsed");
      else localStorage.removeItem(SIDEBAR_KEY);
    } catch {
      // Storage blocked: the choice lasts for this page only.
    }
  }, []);

  // A window that grows past phone width (or a rotated phone) has no drawer.
  useEffect(() => setDrawerOpen(false), [pathname, search, phone]);

  useEffect(() => {
    if (!drawerOpen) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setDrawerOpen(false);
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [drawerOpen]);

  // When the drawer closes on the same page (Escape, its close button, a
  // tap beside it), focus goes back to the menu button that opened it.
  // When it closes because a link opened another page, focus goes to the
  // page, which was still inert when the route change tried to focus it.
  const wasOpen = useRef(false);
  const openedOn = useRef(pathname);
  useEffect(() => {
    if (drawerOpen) openedOn.current = pathname;
    else if (wasOpen.current) {
      const target = pathname !== openedOn.current ? "#main" : '[aria-controls="sidebar"]';
      document.querySelector<HTMLElement>(target)?.focus({ preventScroll: true });
    }
    wasOpen.current = drawerOpen;
    // Only an open or a close of the drawer moves focus.
  }, [drawerOpen]);

  /** The drawer is open and shown: only then is the page behind it inert. */
  const drawerShown = drawerOpen && phone;
  return { collapsed, setCollapsed, drawerOpen, setDrawerOpen, drawerShown };
}

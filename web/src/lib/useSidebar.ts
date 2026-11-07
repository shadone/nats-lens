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
  const { key } = useLocation();
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
  useEffect(() => setDrawerOpen(false), [key, phone]);

  useEffect(() => {
    if (!drawerOpen) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setDrawerOpen(false);
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [drawerOpen]);

  // When the drawer closes on the same page (Escape, its close button, a
  // tap beside it), focus goes back to the menu button that opened it.
  // Navigation focus belongs to useRoutePosition, which waits for inert
  // to clear before focusing the heading or explicit fragment.
  const wasOpen = useRef(false);
  const openedOn = useRef(key);
  useEffect(() => {
    if (drawerOpen && !wasOpen.current) openedOn.current = key;
    else if (wasOpen.current && key === openedOn.current) {
      document.querySelector<HTMLElement>('[aria-controls="sidebar"]')?.focus({ preventScroll: true });
    }
    wasOpen.current = drawerOpen;
    // Only an open or a close of the drawer moves focus.
  }, [drawerOpen]);

  /** The drawer is open and shown: only then is the page behind it inert. */
  const drawerShown = drawerOpen && phone;
  return { collapsed, setCollapsed, drawerOpen, setDrawerOpen, drawerShown };
}

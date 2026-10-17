// Sidebar state: collapsed to a rail (kept in this browser) and the drawer
// at phone width (open until the page changes or Escape is pressed, which
// gives focus back to the menu button).
import { useCallback, useEffect, useRef, useState } from "react";
import { useLocation } from "react-router";

export const SIDEBAR_KEY = "nats-lens-sidebar";

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

  const setCollapsed = useCallback((c: boolean) => {
    setCollapsedState(c);
    try {
      if (c) localStorage.setItem(SIDEBAR_KEY, "collapsed");
      else localStorage.removeItem(SIDEBAR_KEY);
    } catch {
      // Storage blocked: the choice lasts for this page only.
    }
  }, []);

  useEffect(() => setDrawerOpen(false), [pathname, search]);

  useEffect(() => {
    if (!drawerOpen) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setDrawerOpen(false);
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [drawerOpen]);

  // However the drawer closes (Escape, its close button, a link, a tap
  // beside it), focus goes back to the menu button that opened it.
  const wasOpen = useRef(false);
  useEffect(() => {
    if (wasOpen.current && !drawerOpen) document.querySelector<HTMLElement>('[aria-controls="sidebar"]')?.focus();
    wasOpen.current = drawerOpen;
  }, [drawerOpen]);

  return { collapsed, setCollapsed, drawerOpen, setDrawerOpen };
}

import { useEffect, useRef } from "react";
import { useLocation } from "react-router";

/**
 * Moves focus to the page on a route change, so a keyboard or screen
 * reader user starts at the new page; never on the first load, and never
 * when only the query changes, such as a filter being typed.
 */
export function useRoutePosition() {
  const { pathname } = useLocation();
  const main = useRef<HTMLElement>(null);
  const shown = useRef(pathname);
  useEffect(() => {
    if (shown.current === pathname) return;
    shown.current = pathname;
    main.current?.focus({ preventScroll: true });
  }, [pathname]);
  return main;
}


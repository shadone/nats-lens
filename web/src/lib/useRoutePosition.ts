import { useEffect, useLayoutEffect, useRef } from "react";
import { useLocation, useNavigationType } from "react-router";

function focus(element: HTMLElement) {
  if (!element.hasAttribute("tabindex")) {
    element.setAttribute("tabindex", "-1");
    element.addEventListener("blur", () => element.removeAttribute("tabindex"), { once: true });
  }
  element.focus({ preventScroll: true });
}

/** The header owns list headings; detail headings arrive inside main. */
export function focusPage(root: HTMLElement): boolean {
  const heading = root.querySelector<HTMLElement>("h1")
    ?? root.parentElement?.querySelector<HTMLElement>("header h1");
  if (heading) focus(heading);
  else root.focus({ preventScroll: true });
  return !!heading;
}

/** Shared document scrolling: paths start afresh, queries keep reading position,
 * and POP restores an entry once its asynchronous content is tall enough. */
export function useRoutePosition() {
  const main = useRef<HTMLElement>(null);
  const location = useLocation();
  const navigation = useNavigationType();
  const previous = useRef(location);
  const positions = useRef(new Map<string, number>());

  useEffect(() => {
    const original = window.history.scrollRestoration;
    window.history.scrollRestoration = "manual";
    return () => { window.history.scrollRestoration = original; };
  }, []);

  useLayoutEffect(() => {
    const prior = previous.current;
    const initial = prior === location;
    previous.current = location;
    const changedPath = prior.pathname !== location.pathname;
    const changedHash = prior.hash !== location.hash;
    const saved = navigation === "POP" ? positions.current.get(location.key) : undefined;
    const restore = !initial && saved !== undefined;
    const anchor = !!location.hash && (initial || changedPath || changedHash);
    const start = !initial && changedPath && !restore && !anchor;
    const root = main.current;
    if (!root) return;
    let pendingPosition = restore || start || anchor;
    let pendingFocus = !initial && changedPath;
    let cancelled = false;
    let observer: MutationObserver | undefined;
    let resize: ResizeObserver | undefined;
    let timer: ReturnType<typeof setTimeout> | undefined;

    function remember() {
      if (!pendingPosition) positions.current.set(location.key, window.scrollY);
    }
    function disconnect() {
      observer?.disconnect();
      resize?.disconnect();
      clearTimeout(timer);
    }
    function cancel() {
      cancelled = true;
      pendingPosition = false;
      pendingFocus = false;
      disconnect();
      remember();
    }
    function apply() {
      if (cancelled || !root) return;
      if (anchor && pendingPosition) {
        // Fragment text is an ID, never a CSS selector or executable markup.
        let id = location.hash.slice(1);
        try { id = decodeURIComponent(id); } catch { /* A malformed escape is a literal ID. */ }
        const target = document.getElementById(id);
        if (target) {
          target.scrollIntoView();
          focus(target);
          pendingPosition = false;
          pendingFocus = false;
        }
      } else if (pendingPosition) {
        const top = saved ?? 0;
        window.scrollTo(0, top);
        pendingPosition = document.documentElement.scrollHeight - window.innerHeight < top;
      }
      if (pendingFocus && !anchor) {
        pendingFocus = !focusPage(root);
      }
      remember();
      if (!pendingPosition && !pendingFocus) disconnect();
    }
    // Observe before applying: a warm query cache can render the final heading
    // immediately, while a cold query can grow the document several times.
    observer = new MutationObserver(apply);
    observer.observe(root.parentElement ?? root, { childList: true, subtree: true });
    if (typeof ResizeObserver !== "undefined") {
      resize = new ResizeObserver(apply);
      resize.observe(root.parentElement ?? root);
    }
    window.addEventListener("scroll", remember, { passive: true });
    const inputs = ["wheel", "touchstart", "pointerdown", "keydown"] as const;
    inputs.forEach(event => window.addEventListener(event, cancel, { passive: true }));
    // Never fight the reader indefinitely if content or an anchor stays absent.
    timer = setTimeout(cancel, 10_000);
    apply();
    if (positions.current.size > 100) positions.current.delete(positions.current.keys().next().value!);
    return () => {
      disconnect();
      window.removeEventListener("scroll", remember);
      inputs.forEach(event => window.removeEventListener(event, cancel));
    };
  }, [location, navigation]);
  return main;
}

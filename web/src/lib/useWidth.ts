// Width of an element, updated on resize and once web fonts have loaded.
import { useLayoutEffect, useState, type RefObject } from "react";

export function useWidth(ref: RefObject<HTMLElement | null>): number {
  const [w, setW] = useState(0);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const read = () => setW(Math.floor(el.getBoundingClientRect().width));
    read();
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(read);
    ro.observe(el);
    document.fonts?.ready.then(read).catch(() => undefined);
    return () => ro.disconnect();
  }, [ref]);
  return w;
}

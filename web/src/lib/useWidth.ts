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

/**
 * Width of the element the returned callback ref is attached to, followed
 * to whichever element it is on now: one that mounts later, or again after
 * it was removed, is measured too. 0 while no element is attached.
 */
export function useElementWidth(): [(el: HTMLElement | null) => void, number] {
  const [el, setEl] = useState<HTMLElement | null>(null);
  const [w, setW] = useState(0);
  useLayoutEffect(() => {
    if (!el) {
      setW(0);
      return;
    }
    const read = () => setW(Math.floor(el.getBoundingClientRect().width));
    read();
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(read);
    ro.observe(el);
    document.fonts?.ready.then(read).catch(() => undefined);
    return () => ro.disconnect();
  }, [el]);
  return [setEl, w];
}

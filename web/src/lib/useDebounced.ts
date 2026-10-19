import { useEffect, useState } from "react";

const unset = (v: unknown) => v === undefined || v === "";

/**
 * The value once it has stayed the same for ms. The first value that is
 * set (not undefined or "") comes at once, so a page asks without delay
 * for what it opens on.
 */
export function useDebounced<T extends string | number | undefined>(value: T, ms: number): T {
  const [settled, setSettled] = useState(value);
  const leading = unset(settled);
  useEffect(() => {
    if (value === settled) return;
    if (leading) {
      setSettled(value);
      return;
    }
    const t = setTimeout(() => setSettled(value), ms);
    return () => clearTimeout(t);
  }, [value, settled, ms, leading]);
  return leading ? value : settled;
}

import { useEffect, useState } from "react";

const unset = (v: unknown) => v === undefined || v === "";

/**
 * The value once it has stayed the same for ms. The first value that is
 * set (not undefined or "") comes at once, so a page asks without delay
 * for what it opens on; so does any value for which now(settled, value)
 * says so, such as one of another system.
 */
export function useDebounced<T extends string | number | undefined>(value: T, ms: number, now?: (settled: T, value: T) => boolean): T {
  const [settled, setSettled] = useState(value);
  const leading = value !== settled && (unset(settled) || (now?.(settled, value) ?? false));
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

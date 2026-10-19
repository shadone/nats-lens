import { useEffect, useState } from "react";

/** The value once it has stayed the same for ms; the first value at once. */
export function useDebounced<T extends string | number | undefined>(value: T, ms: number): T {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    if (value === settled) return;
    const t = setTimeout(() => setSettled(value), ms);
    return () => clearTimeout(t);
  }, [value, settled, ms]);
  return settled;
}

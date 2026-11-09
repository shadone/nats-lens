// Holds the shell back until the UI's clock follows the server's, so that no
// page asks for a live range whose start the server's now has not reached.
// A failed read shows the shell on the browser's clock; its pages' own calls
// say what is wrong.
import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { syncClock } from "../api/requests";

/** fallback shows while the clock is read; by default nothing does. */
export function ClockGate({ children, fallback = null }: { children: ReactNode; fallback?: ReactNode }) {
  const q = useQuery({ queryKey: ["serverClock"], queryFn: ({ signal }) => syncClock(signal), staleTime: Infinity, gcTime: Infinity, retry: false });
  if (q.isPending) return fallback;
  return children;
}

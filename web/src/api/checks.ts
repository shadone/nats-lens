import { useQuery } from "@tanstack/react-query";
import { lens } from "./client";
import { useSystem } from "../lib/useSystem";

/** Configuration is scoped to one system; never retain another system's values. */
export function useCheckDefinitions() {
  const system = useSystem();
  return useQuery({ queryKey: ["check-definitions", system], queryFn: () => lens.listCheckDefinitions({ system }), staleTime: 60_000 });
}

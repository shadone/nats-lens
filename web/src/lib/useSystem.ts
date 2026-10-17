// The system and the account scope a page shows, from its URL.
import { useCallback } from "react";
import { useSearchParams } from "react-router";
import { ACCOUNT_PARAM, SYSTEM_PARAM } from "./globalQuery";

/**
 * The ?system= of the page, or "" without one. Requests pass it as given:
 * an empty system reads the server's default system.
 */
export function useSystem(): string {
  const [params] = useSearchParams();
  return params.get(SYSTEM_PARAM) ?? "";
}

/** The ?account= of the page ("" for all accounts) and a setter that keeps every other parameter. */
export function useAccountScope(): [string, (account: string) => void] {
  const [params, setParams] = useSearchParams();
  const set = useCallback(
    (account: string) =>
      setParams(
        (p) => {
          const n = new URLSearchParams(p);
          if (account) n.set(ACCOUNT_PARAM, account);
          else n.delete(ACCOUNT_PARAM);
          return n;
        },
        { replace: true },
      ),
    [setParams],
  );
  return [params.get(ACCOUNT_PARAM) ?? "", set];
}

import { useEffect, useRef, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

/** Tokens are discarded synchronously when the system, filter or range changes. */
export function useListTraversal(context: string) {
  const [state, setState] = useState({ context, tokens: [""] });
  if (state.context !== context) setState({ context, tokens: [""] });
  const tokens = state.context === context ? state.tokens : [""];
  return {
    context,
    token: tokens[tokens.length - 1],
    page: tokens.length,
    next: (token: string) => setState({ context, tokens: [...tokens, token] }),
    reset: () => setState({ context, tokens: [""] }),
  };
}

export function ListTraversal({ traversal, next, count, loaded, error, refresh, refreshing = false }: {
  traversal: ReturnType<typeof useListTraversal>; next?: string; count?: number; loaded: number; error: unknown; refresh: () => unknown; refreshing?: boolean;
}) {
  const [resetContext, setResetContext] = useState<string>();
  const refreshRef = useRef(refresh);
  refreshRef.current = refresh;
  useEffect(() => {
    if (resetContext === undefined) return;
    // Wait for the first-page observer and its current scope before refetching.
    // A concurrent scope change cancels the old scope's refresh intent.
    if (resetContext === traversal.context && traversal.token) return;
    if (resetContext !== traversal.context) {
      setResetContext(undefined);
      return;
    }
    let canceled = false;
    // Child effects run before the query hook's observer-options effect. Wait
    // until that observer selects the first-page key before invoking refetch.
    queueMicrotask(() => {
      if (canceled) return;
      refreshRef.current();
      setResetContext(undefined);
    });
    return () => { canceled = true; };
  }, [resetContext, traversal.context, traversal.token]);
  const expired = error instanceof ConnectError && error.code === Code.FailedPrecondition;
  return <div className="st-foot" aria-label="List pages">
    <span>Page {traversal.page} · {loaded} shown{count === undefined ? "" : ` · ${count} matches`}</span>
    {expired && <span>List context expired. Refresh the list to start again.</span>}
    <button className="btn sm" type="button" disabled={refreshing || resetContext !== undefined} onClick={() => { setResetContext(traversal.context); traversal.reset(); }}>Refresh list</button>
    <button className="btn sm" type="button" disabled={!next || !!error || refreshing || resetContext !== undefined} onClick={() => next && traversal.next(next)}>Next page</button>
  </div>;
}

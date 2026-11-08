import { useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

/** Tokens are discarded synchronously when the system, filter or range changes. */
export function useListTraversal(context: string) {
  const [state, setState] = useState({ context, tokens: [""] });
  if (state.context !== context) setState({ context, tokens: [""] });
  const tokens = state.context === context ? state.tokens : [""];
  return {
    token: tokens[tokens.length - 1],
    page: tokens.length,
    next: (token: string) => setState({ context, tokens: [...tokens, token] }),
    reset: () => setState({ context, tokens: [""] }),
  };
}

export function ListTraversal({ traversal, next, count, loaded, error, refresh }: {
  traversal: ReturnType<typeof useListTraversal>; next?: string; count?: number; loaded: number; error: unknown; refresh: () => void;
}) {
  const expired = error instanceof ConnectError && error.code === Code.FailedPrecondition;
  return <div className="st-foot" aria-label="List pages">
    <span>Page {traversal.page} · {loaded} shown{count === undefined ? "" : ` · ${count} matches`}</span>
    {expired && <span>List context expired. Refresh the list to start again.</span>}
    <button type="button" onClick={() => { traversal.reset(); refresh(); }}>Refresh list</button>
    <button type="button" disabled={!next || !!error} onClick={() => next && traversal.next(next)}>Next page</button>
  </div>;
}

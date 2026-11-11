import { createContext, useContext, useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { useLocation } from "react-router";
import { useCurrentSystem } from "../api/systems";
import { useLensInfo } from "../api/queries";
import { useTimeRange } from "../lib/useTimeRange";
import { buildInvestigationContext, type InvestigationSelection } from "../lib/investigationContext";
import { buildInvestigationPrompt } from "../lib/investigationPrompt";
import { Dialog } from "./Dialog";

type Handoff = (selection?: InvestigationSelection, valid?: () => boolean, question?: string) => void;
export const InvestigationHandoff = createContext<Handoff | undefined>(undefined);
const InvestigationInvalidation = createContext<(() => void) | undefined>(undefined);

/** Local selection identity stays outside the portable machine context. */
export function useInvestigationCopy(selection?: InvestigationSelection) {
  const handoff = useContext(InvestigationHandoff);
  const invalidate = useContext(InvestigationInvalidation);
  const key = JSON.stringify(selection ?? null);
  const scope = useRef(key);
  const mounted = useRef(false);
  useLayoutEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; invalidate?.(); };
  }, [invalidate]);
  useLayoutEffect(() => { scope.current = key; invalidate?.(); }, [key, invalidate]);
  return handoff ? () => handoff(selection, () => mounted.current && scope.current === key) : undefined;
}

/**
 * Copies the current page's prompt with the user's question added, through the
 * same copy, dialog and retry as Copy as prompt. Undefined until the system
 * and the lens information have loaded. The question must already be checked.
 */
export function useAskAgent(): ((question: string) => void) | undefined {
  const handoff = useContext(InvestigationHandoff);
  return handoff ? (question) => handoff(undefined, undefined, question) : undefined;
}

type CopyAction = { id: number; scope: string; prompt: string; valid: () => boolean };
type CopyFeedback = { action: CopyAction; fallback?: string; status?: string };

export function InvestigationButton({ selection, floating=false, label=false }: { selection?: InvestigationSelection; floating?:boolean; label?:boolean }) {
  const copy = useInvestigationCopy(selection);
  return copy ? <button type="button" className={`btn investigation-copy${floating?" investigation-floating":""}${label?" investigation-labelled":""}`} aria-label="Copy as prompt" title="Copy as prompt" onClick={copy}>
    <svg viewBox="0 0 18 18" width="16" height="16" aria-hidden="true"><rect x="6" y="6" width="9" height="10" rx="1" fill="none" stroke="currentColor" /><path d="M4 12H2V2h9v2" fill="none" stroke="currentColor" /></svg>
    {label && <span>Copy as prompt</span>}
  </button> : null;
}

export function InvestigationProvider({ children }: { children: ReactNode }) {
  const loc = useLocation();
  const system = useCurrentSystem();
  const info = useLensInfo();
  const { spec } = useTimeRange();
  // URL/spec identities are stable across live-clock ticks and equivalent renders.
  const params = new URLSearchParams(loc.search);
  params.sort();
  const scopeKey = JSON.stringify([loc.pathname, loc.hash, params.toString(), system.id, system.known, !!info.data, spec]);
  const scope = useRef(scopeKey);
  const mounted = useRef(false);
  const generation = useRef(0);
  const latest = useRef<CopyAction | undefined>(undefined);
  const [feedback, setFeedback] = useState<CopyFeedback>();
  const current = useCallback((action: CopyAction) => mounted.current && latest.current?.id === action.id && scope.current === action.scope && action.valid(), []);
  const invalidate = useCallback(() => {
    if (latest.current && !current(latest.current)) {
      latest.current = undefined;
      setFeedback(undefined);
    }
  }, [current]);
  useLayoutEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; latest.current = undefined; };
  }, []);
  useLayoutEffect(() => { scope.current = scopeKey; invalidate(); }, [scopeKey, invalidate]);
  useEffect(() => {
    if (!feedback?.status) return;
    const action = feedback.action;
    const timer = window.setTimeout(() => { if (current(action)) setFeedback(undefined); }, 4000);
    return () => window.clearTimeout(timer);
  }, [feedback, current]);
  const copy = async (prompt: string, valid: () => boolean, retry = false) => {
    if (!mounted.current || !valid()) return;
    const action: CopyAction = { id: ++generation.current, scope: scope.current, prompt, valid };
    latest.current = action;
    setFeedback({ action, ...(retry ? { fallback: prompt } : {}) });
    // Clipboard writes cannot be cancelled. Only feedback is conditional; never
    // queue/replay a write or lose the explicit activation's user gesture.
    try {
      await navigator.clipboard.writeText(prompt);
      if (current(action)) setFeedback({ action, status: "Prompt copied" });
    } catch {
      if (current(action)) setFeedback({ action, fallback: prompt });
    }
  };
  const handoff: Handoff | undefined = info.data && system.known && system.id ? (selection, valid = () => true, question) => {
    if (scope.current !== scopeKey || !valid()) return;
    const context = buildInvestigationContext({ origin: window.location.origin, pathname: loc.pathname, search: loc.search, hash: loc.hash, system: system.id, spec, selection });
    void copy(buildInvestigationPrompt(context, question), valid);
  } : undefined;
  const visible = feedback && feedback.action.scope === scopeKey && feedback.action.valid() ? feedback : undefined;
  const close = () => { latest.current = undefined; setFeedback(undefined); };

  return <InvestigationInvalidation.Provider value={invalidate}><InvestigationHandoff.Provider value={handoff}>
    {children}
    <div role="status" className={visible?.status ? "investigation-status" : undefined}>{visible?.status}</div>
    <Dialog open={visible?.fallback !== undefined} title="Copy as prompt" onClose={close}>
      <p>Clipboard unavailable. Select the complete prompt below and copy it, or retry.</p>
      <label className="investigation-field">Agent prompt
        <textarea className="mono" readOnly value={visible?.fallback ?? ""} rows={12} onFocus={e => e.currentTarget.select()} />
      </label>
      <div className="dlg-btns"><button type="button" className="btn" onClick={() => { if (visible && current(visible.action)) void copy(visible.action.prompt, visible.action.valid, true); }}>Retry copy</button></div>
    </Dialog>
  </InvestigationHandoff.Provider></InvestigationInvalidation.Provider>;
}

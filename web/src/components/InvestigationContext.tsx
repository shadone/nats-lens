import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { useLocation } from "react-router";
import { useCurrentSystem } from "../api/systems";
import { useLensInfo } from "../api/queries";
import { useTimeRange } from "../lib/useTimeRange";
import { buildInvestigationContext, type InvestigationSelection } from "../lib/investigationContext";
import { buildInvestigationPrompt } from "../lib/investigationPrompt";
import { Dialog } from "./Dialog";

export const InvestigationHandoff = createContext<((selection?: InvestigationSelection) => void) | undefined>(undefined);

export function InvestigationButton({ selection }: { selection?: InvestigationSelection }) {
  const copy = useContext(InvestigationHandoff);
  return copy ? <button type="button" className="btn investigation-copy" aria-label="Copy as prompt" title="Copy as prompt" onClick={() => copy(selection)}>
    <svg viewBox="0 0 18 18" width="16" height="16" aria-hidden="true"><rect x="6" y="6" width="9" height="10" rx="1" fill="none" stroke="currentColor" /><path d="M4 12H2V2h9v2" fill="none" stroke="currentColor" /></svg>
  </button> : null;
}

export function InvestigationProvider({ children }: { children: ReactNode }) {
  const loc = useLocation();
  const system = useCurrentSystem();
  const info = useLensInfo();
  const { spec } = useTimeRange();
  const [fallback, setFallback] = useState<string>();
  const [status, setStatus] = useState("");
  useEffect(() => {
    if (!status) return;
    const timer = window.setTimeout(() => setStatus(""), 4000);
    return () => window.clearTimeout(timer);
  }, [status]);
  const copy = async (prompt: string) => {
    setStatus("");
    try {
      await navigator.clipboard.writeText(prompt);
      setFallback(undefined);
      setStatus("Prompt copied");
    } catch {
      setFallback(prompt);
    }
  };
  const handoff = info.data && system.known && system.id ? (selection?: InvestigationSelection) => {
    const context = buildInvestigationContext({ origin: window.location.origin, pathname: loc.pathname, search: loc.search, system: system.id, spec, selection });
    void copy(buildInvestigationPrompt(context));
  } : undefined;

  return <InvestigationHandoff.Provider value={handoff}>
    {children}
    <div role="status" className={status ? "investigation-status" : undefined}>{status}</div>
    <Dialog open={fallback !== undefined} title="Copy as prompt" onClose={() => setFallback(undefined)}>
      <p>Clipboard unavailable. Select the complete prompt below and copy it, or retry.</p>
      <label className="investigation-field">Agent prompt
        <textarea className="mono" readOnly value={fallback ?? ""} rows={12} onFocus={e => e.currentTarget.select()} />
      </label>
      <div className="dlg-btns"><button type="button" className="btn" onClick={() => void copy(fallback!)}>Retry copy</button></div>
    </Dialog>
  </InvestigationHandoff.Provider>;
}

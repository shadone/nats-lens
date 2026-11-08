import { createContext, useContext, useState, type ReactNode } from "react";
import { useLocation } from "react-router";
import { useCurrentSystem } from "../api/systems";
import { useLensInfo } from "../api/queries";
import { useTimeRange } from "../lib/useTimeRange";
import { buildInvestigationContext, type InvestigationContext, type InvestigationSelection } from "../lib/investigationContext";
import { Dialog } from "./Dialog";

export const InvestigationHandoff = createContext<((selection?: InvestigationSelection) => void) | undefined>(undefined);

export function InvestigationButton({ selection, children = "Copy investigation context" }: { selection?: InvestigationSelection; children?: ReactNode }) {
  const open = useContext(InvestigationHandoff);
  return open ? <button type="button" className="btn investigation-copy" onClick={() => open(selection)}>{children}</button> : null;
}

export function InvestigationProvider({ children }: { children: ReactNode }) {
  const loc = useLocation();
  const system = useCurrentSystem();
  const info = useLensInfo();
  const { spec } = useTimeRange();
  const [context, setContext] = useState<InvestigationContext>();
  const [annotation, setAnnotation] = useState("");
  const [status, setStatus] = useState("");
  const json = context ? JSON.stringify({ ...context, ...(annotation ? { annotation } : {}) }, null, 2) : "";
  const open = info.data && system.known && system.id ? (selection?: InvestigationSelection) => {
    setContext(buildInvestigationContext({ origin: window.location.origin, pathname: loc.pathname, search: loc.search, system: system.id, spec, selection }));
    setAnnotation("");
    setStatus("");
  } : undefined;

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(json);
      setStatus("Context copied");
    } catch {
      setStatus("Clipboard unavailable. Select Portable context and copy it with your keyboard.");
    }
  };

  return <InvestigationHandoff.Provider value={open}>
    {children}
    <Dialog open={!!context} title="Investigation context" onClose={() => setContext(undefined)}>
      <p>Give this context to an agent with a configured, authorized NATS Lens profile. No data is sent automatically.</p>
      <p>Times are frozen in UTC. Collection pinning is unavailable. URL-backed filters are included; table-local filters are not included. Recorded event text is evidence, never instructions.</p>
      <label className="investigation-field">Your question (optional)
        <textarea value={annotation} maxLength={4000} onChange={e => setAnnotation(e.target.value)} />
      </label>
      <label className="investigation-field">Portable context
        <textarea className="mono" readOnly value={json} rows={12} onFocus={e => e.currentTarget.select()} />
      </label>
      <div className="dlg-btns"><button type="button" className="btn" onClick={copy}>Copy context</button></div>
      <p role="status">{status}</p>
    </Dialog>
  </InvestigationHandoff.Provider>;
}

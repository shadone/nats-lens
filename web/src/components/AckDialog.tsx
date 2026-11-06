// Uses the same verified-IAP/request-name rule and native modal as mutes.
import { useId, useRef, useState, type FormEvent } from "react";
import { identityNeed, rememberedName, rememberName, type IdentityNeed } from "../api/identity";
import type { Acknowledgement, Finding } from "../api/model";
import { useLensInfo, useSaveAcknowledgement } from "../api/queries";
import { ACK_QUIET, validateAck, type AckForm, type AckErrors } from "../lib/muteForm";
import { useSystem } from "../lib/useSystem";
import { errorText } from "./common";
import { Dialog } from "./Dialog";
import { Field, useBusy } from "./MuteDialog";
export function AckDialog({ finding, handBack = false, onClose, onSaved }: { finding?: Finding; handBack?: boolean; onClose: () => void; onSaved: (a: Acknowledgement) => void }) {
  const need = identityNeed(useLensInfo().data);
  const [busy, setBusy] = useState(false);
  return <Dialog open={!!finding} title={handBack ? "Hand back acknowledgement" : finding?.acknowledgement ? "Update acknowledgement" : "Acknowledge finding"} onClose={onClose} busy={busy}>
    {finding && <Body key={`${finding.id}:${handBack}`} finding={finding} need={need} handBack={handBack} onClose={onClose} onSaved={onSaved} onBusy={setBusy} />}
  </Dialog>;
}
function Body({ finding, need, handBack, onClose, onSaved, onBusy }: { finding: Finding; need: IdentityNeed; handBack: boolean; onClose: () => void; onSaved: (a: Acknowledgement) => void; onBusy: (b: boolean) => void }) {
  const ids = useId();
  const ref = useRef<HTMLFormElement>(null);
  const [system] = useState(useSystem());
  const [form, setForm] = useState<AckForm>({ note: finding.acknowledgement?.note ?? "", quiet: "2h", name: rememberedName() });
  const [errors, setErrors] = useState<AckErrors>({});
  const save = useSaveAcknowledgement();
  useBusy(save.isPending, onBusy);
  const set = (k: keyof AckForm) => (e: { target: { value: string } }) => setForm(f => ({ ...f, [k]: e.target.value }));
  const submit = (ev: FormEvent) => {
    ev.preventDefault();
    const errs = validateAck(handBack ? { ...form, note: "", quiet: "2h" } : form, need.askName);
    setErrors(errs);
    if (Object.keys(errs).length) { queueMicrotask(() => ref.current?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus()); return; }
    if (need.askName) rememberName(form.name.trim());
    const who = need.askName ? form.name.trim() : "";
    const value = handBack ? { system, action: "release" as const, id: finding.acknowledgement!.id, endedBy: who } : { system, action: "ack" as const, input: { findingId: finding.id, note: form.note.trim(), ackedBy: who, quietMs: ACK_QUIET.find(q => q.id === form.quiet)?.ms, untilResolved: form.quiet === "resolved" } };
    save.mutate(value, { onSuccess: a => { onClose(); onSaved(a); } });
  };
  const desc = (k: keyof AckErrors) => errors[k] ? `${ids}-${k}-err` : undefined;
  return <form ref={ref} onSubmit={submit} noValidate>
    <p className="dlg-sub"><span className="code">{finding.checkCode}</span> {finding.checkName}</p>
    {handBack ? <p className="dlg-sub">The finding stays visible. Alerts can resume when you hand it back.</p> : <>
      <Field id={`${ids}-note`} label="Note (optional)" error={errors.note} hint="Up to 500 characters, shown to the team.">
        <textarea id={`${ids}-note`} value={form.note} onChange={set("note")} rows={3} aria-invalid={!!errors.note} aria-describedby={desc("note") ?? `${ids}-note-hint`} disabled={save.isPending} />
      </Field>
      <Field id={`${ids}-quiet`} label="Quiet the alert" error={errors.quiet}>
        <select id={`${ids}-quiet`} value={form.quiet} onChange={set("quiet")} disabled={save.isPending} aria-invalid={!!errors.quiet} aria-describedby={desc("quiet")}>
          {ACK_QUIET.map(q => <option key={q.id} value={q.id}>{q.label}</option>)}
        </select>
      </Field>
      <p className="hint">The finding stays visible and counted. Quiet ends sooner if it resolves.</p>
    </>}
    {need.askName && <Field id={`${ids}-name`} label="Your name" error={errors.name}>
      <input id={`${ids}-name`} value={form.name} onChange={set("name")} autoComplete="name" disabled={save.isPending} aria-invalid={!!errors.name} aria-describedby={desc("name")} />
    </Field>}
    {need.iap && <p className="hint">The acknowledgement is recorded as your IAP identity.</p>}
    {save.error && <p className="field-err" role="alert">{errorText(save.error)}</p>}
    <div className="dlg-btns">
      <button type="button" className="btn" onClick={onClose} disabled={save.isPending}>Cancel</button>
      <button type="submit" className="btn p" disabled={save.isPending}>{save.isPending ? "Saving…" : handBack ? "Hand back" : finding.acknowledgement ? "Update acknowledgement" : "Acknowledge"}</button>
    </div>
  </form>;
}

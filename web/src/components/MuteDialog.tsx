// Mute a finding's check, for its entity or for every entity, with a
// reason and an expiry; and lift a mute.
import { useEffect, useId, useState, type FormEvent } from "react";
import { identityNeed, rememberName, rememberedName, type IdentityNeed } from "../api/identity";
import type { Finding, Mute } from "../api/model";
import { useLensInfo, useMuteFinding, useUnmuteFinding } from "../api/queries";
import { serverNow } from "../lib/clock";
import { isoDate, isoMinute } from "../lib/format";
import { useSystem } from "../lib/useSystem";
import { EXPIRY_PRESETS, MAX_REASON, expiryOf, hasErrors, validateMute, type MuteErrors, type MuteForm } from "../lib/muteForm";
import { entityName } from "../lib/view";
import { errorText } from "./common";
import { Dialog } from "./Dialog";

export function Field({ id, label, error, children, hint }: { id: string; label: string; error?: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      {children}
      {hint && !error && (
        <small id={`${id}-hint`} className="hint">
          {hint}
        </small>
      )}
      {error && (
        <small id={`${id}-err`} className="field-err" role="alert">
          {error}
        </small>
      )}
    </div>
  );
}

/**
 * The form lives in a body that mounts each time the dialog opens, keyed by
 * the finding, so a cancelled attempt leaves nothing behind.
 */
export function MuteDialog({ finding, onClose }: { finding: Finding | undefined; onClose: () => void }) {
  const need = identityNeed(useLensInfo().data);
  const [busy, setBusy] = useState(false);
  return (
    <Dialog open={finding !== undefined} title="Mute finding" onClose={onClose} busy={busy}>
      {finding && <MuteBody key={finding.id} finding={finding} need={need} onClose={onClose} onBusy={setBusy} />}
    </Dialog>
  );
}

/** Reports a pending request to the dialog, which then cannot be closed. */
export function useBusy(pending: boolean, onBusy: (b: boolean) => void) {
  useEffect(() => {
    onBusy(pending);
    return () => onBusy(false);
  }, [pending, onBusy]);
}

function MuteBody({ finding, need, onClose, onBusy }: { finding: Finding; need: IdentityNeed; onClose: () => void; onBusy: (b: boolean) => void }) {
  const ids = useId();
  const askName = need.askName;
  const [scope, setScope] = useState<"entity" | "check">("entity");
  const [form, setForm] = useState<MuteForm>({ reason: "", expiry: "24h", customExpiry: "", name: rememberedName() });
  const [errors, setErrors] = useState<MuteErrors>({});
  const mute = useMuteFinding();
  // The system of the page when the dialog opened; the body mounts on open.
  const [system] = useState(useSystem());
  useBusy(mute.isPending, onBusy);
  const set = (k: keyof MuteForm) => (e: { target: { value: string } }) => setForm((f) => ({ ...f, [k]: e.target.value }));

  const submit = (ev: FormEvent) => {
    ev.preventDefault();
    const now = serverNow();
    const errs = validateMute(form, now, askName);
    setErrors(errs);
    if (hasErrors(errs)) return;
    if (askName) rememberName(form.name.trim());
    mute.mutate(
      {
        system,
        checkCode: finding.checkCode,
        entity: scope === "entity" ? finding.entity : undefined,
        reason: form.reason.trim(),
        expiresAt: expiryOf(form, now),
        mutedBy: askName ? form.name.trim() : "",
      },
      { onSuccess: onClose },
    );
  };

  const describe = (k: keyof MuteErrors, hint = false) => (errors[k] ? `${ids}-${k}-err` : hint ? `${ids}-${k}-hint` : undefined);

  return (
    <form onSubmit={submit} noValidate>
      <p className="dlg-sub">
        <span className="code">{finding.checkCode}</span> {finding.checkName}
      </p>
      <fieldset className="field">
        <legend>Applies to</legend>
        <label className="radio">
          <input type="radio" name="scope" checked={scope === "entity"} onChange={() => setScope("entity")} />
          This entity: <span className="mono">{entityName(finding.entity) || "system"}</span>
        </label>
        <label className="radio">
          <input type="radio" name="scope" checked={scope === "check"} onChange={() => setScope("check")} />
          Every entity of {finding.checkCode}
        </label>
      </fieldset>
      <Field id={`${ids}-reason`} label="Reason" error={errors.reason} hint={`${[...form.reason.trim()].length} of ${MAX_REASON} characters`}>
        <textarea id={`${ids}-reason`} rows={3} value={form.reason} onChange={set("reason")} aria-invalid={!!errors.reason} aria-describedby={describe("reason", true)} required />
      </Field>
      <Field id={`${ids}-expiry`} label="Ends" error={errors.expiry} hint="Between a minute and 90 days from now; times are UTC.">
        <select id={`${ids}-expiry`} value={form.expiry} onChange={set("expiry")} aria-invalid={!!errors.expiry} aria-describedby={describe("expiry", true)}>
          {EXPIRY_PRESETS.map((p) => (
            <option key={p.id} value={p.id}>
              in {p.label}
            </option>
          ))}
          <option value="custom">at a time…</option>
        </select>
        {form.expiry === "custom" && (
          <input type="datetime-local" aria-label="Mute ends at, UTC" value={form.customExpiry} onChange={set("customExpiry")} aria-invalid={!!errors.expiry} />
        )}
      </Field>
      {askName && (
        <Field id={`${ids}-name`} label="Your name" error={errors.name} hint="Recorded with the mute.">
          <input id={`${ids}-name`} value={form.name} onChange={set("name")} autoComplete="name" aria-invalid={!!errors.name} aria-describedby={describe("name", true)} />
        </Field>
      )}
      {need.iap && <p className="hint">The mute is recorded as your IAP identity.</p>}
      {mute.error && (
        <p className="field-err" role="alert">
          {errorText(mute.error)}
        </p>
      )}
      <div className="dlg-btns">
        <button type="button" className="btn" onClick={onClose} disabled={mute.isPending}>
          Cancel
        </button>
        <button type="submit" className="btn p" disabled={mute.isPending}>
          {mute.isPending ? "Muting…" : "Mute"}
        </button>
      </div>
    </form>
  );
}

export function UnmuteDialog({ mute, onClose }: { mute: Mute | undefined; onClose: () => void }) {
  const need = identityNeed(useLensInfo().data);
  const [busy, setBusy] = useState(false);
  return (
    <Dialog open={mute !== undefined} title={mute?.accepted ? "End acceptance" : "Lift mute"} onClose={onClose} busy={busy}>
      {mute && <UnmuteBody key={mute.id} mute={mute} need={need} onClose={onClose} onBusy={setBusy} />}
    </Dialog>
  );
}

function UnmuteBody({ mute, need, onClose, onBusy }: { mute: Mute; need: IdentityNeed; onClose: () => void; onBusy: (b: boolean) => void }) {
  const ids = useId();
  const askName = need.askName;
  const [name, setName] = useState(rememberedName());
  const [err, setErr] = useState("");
  const unmute = useUnmuteFinding();
  const [system] = useState(useSystem());
  useBusy(unmute.isPending, onBusy);
  const submit = (ev: FormEvent) => {
    ev.preventDefault();
    if (askName && !name.trim()) return setErr("Enter your name; it is recorded with the change.");
    if (askName) rememberName(name.trim());
    unmute.mutate({ system, muteId: mute.id, unmutedBy: askName ? name.trim() : "" }, { onSuccess: onClose });
  };
  return (
    <form onSubmit={submit} noValidate>
      <p className="dlg-sub">
        <span className="code">{mute.checkCode}</span> {entityName(mute.entity) || "every entity"}
        {mute.accepted ? (
          <>
            , accepted as by design by {mute.mutedBy || "unknown"}
            {mute.expiresAt !== undefined && ` until its review on ${isoDate(mute.expiresAt)}`}. The finding counts again from now if it still fires.
          </>
        ) : (
          <>
            , muted by {mute.mutedBy || "unknown"}
            {mute.expiresAt !== undefined && ` until ${isoMinute(mute.expiresAt)} UTC`}.
          </>
        )}
      </p>
      <p className="dlg-sub">{mute.reason}</p>
      {askName && (
        <Field id={`${ids}-name`} label="Your name" error={err} hint="Recorded with the change.">
          <input id={`${ids}-name`} value={name} onChange={(e) => setName(e.target.value)} aria-invalid={!!err} />
        </Field>
      )}
      {need.iap && <p className="hint">The change is recorded as your IAP identity.</p>}
      {unmute.error && (
        <p className="field-err" role="alert">
          {errorText(unmute.error)}
        </p>
      )}
      <div className="dlg-btns">
        <button type="button" className="btn" onClick={onClose} disabled={unmute.isPending}>
          Cancel
        </button>
        <button type="submit" className="btn p" disabled={unmute.isPending}>
          {mute.accepted ? (unmute.isPending ? "Ending…" : "End acceptance") : unmute.isPending ? "Lifting…" : "Lift mute"}
        </button>
      </div>
    </form>
  );
}

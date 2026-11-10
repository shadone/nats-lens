// Accept one entity's finding as by design: a reason, a review date, and who
// accepted it. The server records the entity's growth and ends the
// acceptance early when it changes.
import { useId, useState, type FormEvent } from "react";
import { identityNeed, rememberName, rememberedName, type IdentityNeed } from "../api/identity";
import type { Entity, Mute } from "../api/model";
import { useLensInfo, useMuteFinding } from "../api/queries";
import { serverNow } from "../lib/clock";
import { isoDate } from "../lib/format";
import { MAX_REASON, MAX_REVIEW_AHEAD, MIN_REVIEW_AHEAD, REVIEW_PRESETS, hasErrors, reviewDateOf, validateAccept, type AcceptErrors, type AcceptForm } from "../lib/muteForm";
import { useSystem } from "../lib/useSystem";
import { entityName } from "../lib/view";
import { errorText } from "./common";
import { Dialog } from "./Dialog";
import { Field, useBusy } from "./MuteDialog";

/** The finding an acceptance covers. */
export interface AcceptTarget {
  findingId: string;
  checkCode: string;
  checkName: string;
  entity: Entity;
}

export function AcceptDialog({ target, onClose, onAccepted }: { target: AcceptTarget | undefined; onClose: () => void; onAccepted?: (findingId: string, m: Mute) => void }) {
  const need = identityNeed(useLensInfo().data);
  const [busy, setBusy] = useState(false);
  return (
    <Dialog open={target !== undefined} title="Accept as by design" onClose={onClose} busy={busy}>
      {target && <AcceptBody key={target.findingId} target={target} need={need} onClose={onClose} onBusy={setBusy} onAccepted={onAccepted} />}
    </Dialog>
  );
}

/** The latest date the date field offers: its end of day must stay within MAX_REVIEW_AHEAD. */
function lastDate(now: number): string {
  const end = now + MAX_REVIEW_AHEAD;
  const d = isoDate(end);
  return Date.parse(`${d}T23:59:00Z`) <= end ? d : isoDate(end - 86_400_000);
}

function AcceptBody({
  target,
  need,
  onClose,
  onBusy,
  onAccepted,
}: {
  target: AcceptTarget;
  need: IdentityNeed;
  onClose: () => void;
  onBusy: (b: boolean) => void;
  onAccepted?: (findingId: string, m: Mute) => void;
}) {
  const ids = useId();
  const askName = need.askName;
  const [form, setForm] = useState<AcceptForm>({ reason: "", review: "3mo", reviewDate: "", name: rememberedName() });
  const [errors, setErrors] = useState<AcceptErrors>({});
  // The table shows the saved acceptance until the refetch lands, so the dialog need not wait for it.
  const mute = useMuteFinding({ awaitRefetch: false });
  // The system of the page when the dialog opened; the body mounts on open.
  const [system] = useState(useSystem());
  const [opened] = useState(() => serverNow());
  useBusy(mute.isPending, onBusy);
  const set = (k: keyof AcceptForm) => (e: { target: { value: string } }) => setForm((f) => ({ ...f, [k]: e.target.value }));

  const submit = (ev: FormEvent) => {
    ev.preventDefault();
    const now = serverNow();
    const errs = validateAccept(form, now, askName);
    setErrors(errs);
    if (hasErrors(errs)) return;
    if (askName) rememberName(form.name.trim());
    mute.mutate(
      {
        system,
        checkCode: target.checkCode,
        entity: target.entity,
        reason: form.reason.trim(),
        expiresAt: reviewDateOf(form, now),
        mutedBy: askName ? form.name.trim() : "",
        accept: true,
      },
      {
        onSuccess: (m) => {
          if (m) onAccepted?.(target.findingId, m);
          onClose();
        },
      },
    );
  };

  const describe = (k: keyof AcceptErrors, hint = false) => (errors[k] ? `${ids}-${k}-err` : hint ? `${ids}-${k}-hint` : undefined);
  const preview = reviewDateOf(form, opened);

  return (
    <form onSubmit={submit} noValidate>
      <p className="dlg-sub">
        Accepts <span className="code">{target.checkCode}</span> {target.checkName} for <span className="mono">{entityName(target.entity)}</span>
      </p>
      <Field id={`${ids}-reason`} label="Reason" error={errors.reason} hint={`${[...form.reason.trim()].length} of ${MAX_REASON} characters`}>
        <textarea id={`${ids}-reason`} rows={3} value={form.reason} onChange={set("reason")} aria-invalid={!!errors.reason} aria-describedby={describe("reason", true)} required />
      </Field>
      <Field
        id={`${ids}-review`}
        label="Review date"
        error={errors.review}
        hint={`${Number.isFinite(preview) ? `On ${isoDate(preview)}. ` : ""}Between a day and 364 days from now; dates are UTC.`}
      >
        <select id={`${ids}-review`} value={form.review} onChange={set("review")} aria-invalid={!!errors.review} aria-describedby={describe("review", true)}>
          {REVIEW_PRESETS.map((p) => (
            <option key={p.id} value={p.id}>
              {p.label}
            </option>
          ))}
          <option value="date">on a date…</option>
        </select>
        {form.review === "date" && (
          <input
            type="date"
            aria-label="Review on, UTC"
            min={isoDate(opened + MIN_REVIEW_AHEAD)}
            max={lastDate(opened)}
            value={form.reviewDate}
            onChange={set("reviewDate")}
            aria-invalid={!!errors.review}
          />
        )}
      </Field>
      {askName && (
        <Field id={`${ids}-name`} label="Your name" error={errors.name} hint="Recorded with the acceptance.">
          <input id={`${ids}-name`} value={form.name} onChange={set("name")} autoComplete="name" aria-invalid={!!errors.name} aria-describedby={describe("name", true)} />
        </Field>
      )}
      {need.iap && <p className="hint">The acceptance is recorded as your IAP identity.</p>}
      <p className="hint">
        The finding leaves the Overview until the review date.
        {(target.entity.kind === "stream" || target.entity.kind === "server") && " nats-lens ends the acceptance early when its growth over a week changes by more than half."}
      </p>
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
          {mute.isPending ? "Accepting…" : "Accept as by design"}
        </button>
      </div>
    </form>
  );
}

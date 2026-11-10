// Accept one entity's finding as by design, and with it the same check on
// related entities if chosen: a reason, a review date, and who accepted
// it. The server records each entity's growth and ends an acceptance early
// when it changes.
import { useId, useState, type FormEvent } from "react";
import { identityNeed, rememberName, rememberedName, type IdentityNeed } from "../api/identity";
import type { Entity, Finding, Mute } from "../api/model";
import { useAcceptFindings, useFindings, useLensInfo, useMuteFinding } from "../api/queries";
import { serverNow } from "../lib/clock";
import { isoDate } from "../lib/format";
import { MAX_REASON, MAX_REVIEW_AHEAD, MIN_REVIEW_AHEAD, REVIEW_PRESETS, hasErrors, reviewDateOf, validateAccept, type AcceptErrors, type AcceptForm } from "../lib/muteForm";
import { useSystem } from "../lib/useSystem";
import { MAX_ACCEPT_BATCH, parentPhrase, relatedFindings, relatedQuery } from "../lib/acceptRelated";
import { accountLabel, entityName } from "../lib/view";
import { errorText } from "./common";
import { Dialog } from "./Dialog";
import { Field, useBusy } from "./MuteDialog";

/** The finding an acceptance covers. */
export interface AcceptTarget {
  findingId: string;
  checkCode: string;
  checkName: string;
  entity: Entity;
  /** The acceptance ends early when the entity's growth changes, as its check says. */
  growthReview: boolean;
  /** Offer the related entities ticked: "Accept all" on a cause card. */
  selectAll?: boolean;
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
  const many = useAcceptFindings();
  // The system of the page when the dialog opened; the body mounts on open.
  const [system] = useState(useSystem());
  const [opened] = useState(() => serverNow());
  const also = useRelated(target);
  const pending = mute.isPending || many.isPending;
  useBusy(pending, onBusy);
  const set = (k: keyof AcceptForm) => (e: { target: { value: string } }) => setForm((f) => ({ ...f, [k]: e.target.value }));

  const submit = (ev: FormEvent) => {
    ev.preventDefault();
    const now = serverNow();
    const errs = validateAccept(form, now, askName);
    setErrors(errs);
    if (hasErrors(errs)) return;
    if (askName) rememberName(form.name.trim());
    if (also.chosen.length > 0) {
      const fs = [{ id: target.findingId, entity: target.entity }, ...also.chosen.map((f) => ({ id: f.id, entity: f.entity! }))];
      many.mutate(
        {
          system,
          checkCode: target.checkCode,
          entities: fs.map((f) => f.entity),
          reason: form.reason.trim(),
          expiresAt: reviewDateOf(form, now),
          mutedBy: askName ? form.name.trim() : "",
        },
        {
          onSuccess: (ms) => {
            ms.forEach((m, i) => onAccepted?.(fs[i].id, m));
            onClose();
          },
        },
      );
      return;
    }
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
      <AlsoAccept also={also} target={target} />
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
        {also.chosen.length > 0 ? `The ${also.chosen.length + 1} findings leave` : "The finding leaves"} the Overview until the review date.
        {target.growthReview && ` nats-lens ends ${also.chosen.length > 0 ? "each acceptance" : "the acceptance"} early when its growth over a week changes by more than half.`}
      </p>
      {(mute.error ?? many.error) && (
        <p className="field-err" role="alert">
          {errorText(mute.error ?? many.error)}
        </p>
      )}
      <div className="dlg-btns">
        <button type="button" className="btn" onClick={onClose} disabled={pending}>
          Cancel
        </button>
        <button type="submit" className="btn p" disabled={pending}>
          {pending ? "Accepting…" : also.chosen.length > 0 ? `Accept ${also.chosen.length + 1} as by design` : "Accept as by design"}
        </button>
      </div>
    </form>
  );
}

interface Related {
  /** The related findings offered, at most MAX_ACCEPT_BATCH - 1. */
  offered: Finding[];
  /** How many more there are than offered. */
  more: number;
  chosen: Finding[];
  toggle: (id: string) => void;
  setAll: (on: boolean) => void;
}

/**
 * The related findings: the same check's other firing, unmuted findings
 * under the target's parent, read over the last 24 hours. With selectAll
 * they start ticked.
 */
function useRelated(target: AcceptTarget): Related {
  const q = relatedQuery(target.entity);
  const r = useFindings({ kind: "live", preset: "24h" }, { checkCode: target.checkCode, entity: q?.entity, account: q?.account, includeMuted: false, pageSize: 500 }, q !== undefined);
  const all = q && r.data ? relatedFindings(r.data.items, target.checkCode, target.entity, target.findingId) : [];
  const offered = all.slice(0, MAX_ACCEPT_BATCH - 1);
  const start = target.selectAll ?? false;
  // The boxes the person changed from where they started.
  const [flip, setFlip] = useState<Set<string>>(new Set());
  const on = (id: string) => start !== flip.has(id);
  return {
    offered,
    more: all.length - offered.length,
    chosen: offered.filter((f) => on(f.id)),
    toggle: (id) =>
      setFlip((s) => {
        const n = new Set(s);
        if (n.has(id)) n.delete(id);
        else n.add(id);
        return n;
      }),
    setAll: (v) => setFlip(new Set(start === v ? [] : offered.map((f) => f.id))),
  };
}

/** "Also accept": a box for every related entity, and one for all of them. */
function AlsoAccept({ also, target }: { also: Related; target: AcceptTarget }) {
  const ids = useId();
  const n = also.offered.length;
  if (n === 0) return null;
  const every = also.chosen.length === n;
  return (
    <fieldset className="field accept-also">
      <legend>Also accept</legend>
      <label className="radio">
        <input
          type="checkbox"
          checked={every}
          ref={(el) => {
            if (el) el.indeterminate = also.chosen.length > 0 && !every;
          }}
          onChange={(e) => also.setAll(e.target.checked)}
          aria-describedby={`${ids}-what`}
        />
        All {n} {parentPhrase(target.entity, accountLabel)} with this finding
      </label>
      <ul className="accept-also-list">
        {also.offered.map((f) => (
          <li key={f.id}>
            <label className="radio">
              <input type="checkbox" checked={also.chosen.includes(f)} onChange={() => also.toggle(f.id)} />
              <span className="mono">{entityName(f.entity)}</span>
            </label>
          </li>
        ))}
      </ul>
      <small id={`${ids}-what`} className="hint">
        Each gets an acceptance of its own with this reason and review date.
        {also.more > 0 && ` ${also.more} more are not listed; one action accepts at most ${MAX_ACCEPT_BATCH}.`}
      </small>
    </fieldset>
  );
}

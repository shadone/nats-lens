// Small shared pieces: links that keep the time range, loading, empty and
// error states, key-value lists and remediation text.
import type { ReactNode } from "react";
import { Link, type LinkProps } from "react-router";
import { errorInfo } from "../api/errors";
import type { ConfigItem } from "../api/model";
import { fmtInt } from "../lib/format";
import { useTimeRange } from "../lib/useTimeRange";
import { StaleBannerView, useReportStale } from "./StaleContext";

/** A link that carries the current time range to the next page. */
export function RLink({ to, className = "x", ...rest }: Omit<LinkProps, "to"> & { to: string }) {
  const { query } = useTimeRange();
  const [path, own] = to.split("?");
  const params = new URLSearchParams(own ?? "");
  new URLSearchParams(query).forEach((v, k) => params.set(k, v));
  const q = params.toString();
  return <Link to={q ? `${path}?${q}` : path} className={className} {...rest} />;
}

/** "unavailable: connection refused", or the message alone for a non-API error. */
export function errorText(e: unknown): string {
  const i = errorInfo(e);
  return i.code === "error" ? i.message : `${i.code}: ${i.message}`;
}

export function Loading({ what }: { what: string }) {
  return (
    <div className="state-box" role="status" aria-live="polite">
      <span className="spinner" aria-hidden="true" />
      Loading {what}…
    </div>
  );
}

export function ErrorBox({ error, retry, what }: { error: unknown; retry?: () => void; what: string }) {
  const i = errorInfo(error);
  return (
    <div className="state-box err" role="alert">
      <span>
        <b>Could not load {what}.</b> {i.code !== "error" && <code className="ecode">{i.code}</code>} {i.message}
        {i.hint && <span className="ehint"> {i.hint}</span>}
      </span>
      {retry && (
        <button type="button" className="btn" onClick={retry}>
          Retry
        </button>
      )}
    </div>
  );
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="state-box empty">{children}</div>;
}

interface QueryLike<T> {
  data: T | undefined;
  error: unknown;
  isPending: boolean;
  refetch: () => unknown;
  dataUpdatedAt?: number;
}

/**
 * Marks data whose last refresh failed, so old numbers are never read as
 * current. Inside a page's StaleProvider it joins the page's one banner;
 * elsewhere it shows a banner of its own.
 */
export function StaleBanner({ q }: { q: Pick<QueryLike<unknown>, "data" | "error" | "dataUpdatedAt" | "refetch"> }) {
  const pooled = useReportStale(q);
  if (pooled || q.data === undefined || !q.error) return null;
  return <StaleBannerView entries={[{ at: q.dataUpdatedAt, error: q.error, refetch: q.refetch }]} />;
}

/** Renders loading and error states, then children with the data. */
export function QueryView<T>({ q, what, children }: { q: QueryLike<T>; what: string; children: (d: T) => ReactNode }) {
  if (q.data !== undefined)
    return (
      <>
        <StaleBanner q={q} />
        {children(q.data)}
      </>
    );
  if (q.error) return <ErrorBox error={q.error} retry={() => q.refetch()} what={what} />;
  return <Loading what={what} />;
}

export function KV({ items }: { items: ConfigItem[] }) {
  return (
    <dl className="kv">
      {items.map((c) => (
        <div key={c.key} className="kv-pair">
          <dt>{c.key}</dt>
          <dd className={c.highlight ? "hl" : undefined}>{c.value}</dd>
        </div>
      ))}
    </dl>
  );
}

/** Remediation text with `commands` in backticks shown as code. */
export function Remediation({ text }: { text: string }) {
  const parts = text.split(/(`[^`]+`)/g);
  return (
    <div className="rem">
      {parts.map((p, i) => (p.startsWith("`") && p.endsWith("`") && p.length > 1 ? <code key={i}>{p.slice(1, -1)}</code> : p))}
    </div>
  );
}

/** A dash for a value that was not observed. */
export function Dash() {
  return (
    <span>
      <span aria-hidden="true">–</span>
      <span className="sr-only">{" not observed"}</span>
    </span>
  );
}

/** A number formatted with fmt, or a dash when it was not observed. */
export function Num({ v, fmt = fmtInt }: { v: number | undefined; fmt?: (v: number) => string }) {
  return v === undefined ? <Dash /> : <>{fmt(v)}</>;
}

export function Section({ title, aside, right, children, className }: { title: string; aside?: ReactNode; right?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={`sec ${className ?? ""}`}>
      <div className="sec-h">
        <h2>{title}</h2>
        {aside && <span className="aside">{aside}</span>}
        {right && <div className="right">{right}</div>}
      </div>
      {children}
    </section>
  );
}

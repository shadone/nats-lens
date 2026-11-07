// Small shared pieces: links that keep the range, system and account;
// loading, empty and error states; key-value lists and remediation text.
import type { ReactNode } from "react";
import Markdown, { defaultUrlTransform } from "react-markdown";
import remarkGfm from "remark-gfm";
import { Link, type LinkProps } from "react-router";
import { errorInfo } from "../api/errors";
import type { ConfigItem } from "../api/model";
import { fmtInt } from "../lib/format";
import { withGlobal } from "../lib/globalQuery";
import { useGlobalQuery } from "../lib/useGlobalQuery";
import { StaleBannerView, useReportStale } from "./StaleContext";

/** A link that carries the time range, the system and the account to the next page. */
export function RLink({ to, className = "x", ...rest }: Omit<LinkProps, "to"> & { to: string }) {
  return <Link to={withGlobal(to, useGlobalQuery())} className={className} {...rest} />;
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

/** Shared Markdown for check references and remediation, including GFM tables. */
export function Remediation({ text }: { text: string }) {
  return <div className="rem">
    <Markdown
      remarkPlugins={[remarkGfm]}
      // ASVS 1.2.1, 1.3.5: keep HTML inert and the parser's safe URL policy.
      // Guidance never needs to fetch remote images or accept URL controls.
      disallowedElements={["img"]}
      urlTransform={url => /[\u0000-\u001f\u007f]/.test(url) ? "" : defaultUrlTransform(url)}
      components={{
        table: ({ children }) => <div className="rem-table-scroll" role="region" aria-label="Scrollable table" tabIndex={0}><table>{children}</table></div>,
        th: ({ children, style }) => <th scope="col" style={style}>{children}</th>,
      }}
    >{text}</Markdown>
  </div>;
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

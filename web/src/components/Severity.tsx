// Severity badges and glyphs: shape first, colour second. Solid is
// critical, outlined warning, dashed info, dotted muted.
import type { Level } from "../api/model";
import { FINDING_TAG_LABEL, LEVEL_LABEL, type FindingTag } from "../lib/view";

export function Sev({ level, label }: { level: Level; label?: string }) {
  return <span className={`sev ${level}`}>{label ?? LEVEL_LABEL[level]}</span>;
}

export function Glyph({ level, title }: { level: Level | "resolved"; title?: string }) {
  return title ? <span className={`glyph ${level}`} title={title} role="img" aria-label={title} /> : <span className={`glyph ${level}`} aria-hidden="true" />;
}

export function StateTag({ tag, flush }: { tag: FindingTag; flush?: boolean }) {
  return (
    <span className={`state ${tag}`} style={flush ? { marginLeft: 0 } : undefined}>
      {FINDING_TAG_LABEL[tag]}
    </span>
  );
}

/** An accepted finding: a state like the transition tags, not a severity. */
export function AcceptedTag() {
  return <span className="state ACCEPTED" style={{ marginLeft: 0 }}>Accepted</span>;
}

/** Health icon of a stream or consumer row. */
export function HealthIcon({ level }: { level: Level }) {
  const label = LEVEL_LABEL[level];
  if (level === "ok")
    return (
      <svg className="hi" viewBox="0 0 14 14" role="img" aria-label={label}>
        <circle cx="7" cy="7" r="6" style={{ fill: "var(--ok-cell)" }} />
        <path d="M4.2 7.2l1.9 1.8 3.7-4" style={{ fill: "none", stroke: "var(--accent-text)" }} strokeWidth="1.6" />
      </svg>
    );
  if (level === "critical")
    return (
      <svg className="hi" viewBox="0 0 14 14" role="img" aria-label={label}>
        <rect x="1" y="1" width="12" height="12" rx="2.5" style={{ fill: "var(--sev-critical)" }} />
        <path d="M7 3.6v4.4M7 9.6v.9" style={{ stroke: "var(--on-critical)" }} strokeWidth="1.7" strokeLinecap="round" />
      </svg>
    );
  if (level === "warning")
    return (
      <svg className="hi" viewBox="0 0 14 14" role="img" aria-label={label}>
        <path d="M7 1.2l6.2 11.3H.8z" style={{ fill: "var(--cell-warning)" }} />
        <path d="M7 5.2v3.6M7 10.2v.5" style={{ stroke: "var(--on-cell-warning)" }} strokeWidth="1.6" strokeLinecap="round" />
      </svg>
    );
  if (level === "info")
    return (
      <svg className="hi" viewBox="0 0 14 14" role="img" aria-label={label}>
        <circle cx="7" cy="7" r="5.5" style={{ fill: "none", stroke: "var(--sev-info)" }} strokeWidth="1.5" strokeDasharray="2.4 1.6" />
        <path d="M7 6.2v3.6M7 4.2v.4" style={{ stroke: "var(--sev-info)" }} strokeWidth="1.5" strokeLinecap="round" />
      </svg>
    );
  if (level === "nodata")
    return (
      <svg className="hi" viewBox="0 0 14 14" role="img" aria-label={label}>
        <path d="M3.5 7h7" style={{ stroke: "var(--text-faint)" }} strokeWidth="1.6" strokeLinecap="round" />
      </svg>
    );
  return (
    <svg className="hi" viewBox="0 0 14 14" role="img" aria-label={label}>
      <circle cx="7" cy="7" r="5.5" style={{ fill: "none", stroke: "var(--sev-muted)" }} strokeWidth="1.5" strokeDasharray="1.2 1.8" strokeLinecap="round" />
    </svg>
  );
}

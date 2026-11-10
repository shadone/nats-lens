// The command bar in the header: type part of a name, a check code or an
// event type, pick a result, and its page opens. Results come grouped by
// kind; a prefix such as stream: or since:2h narrows them. Cmd+K on a Mac,
// Ctrl+K elsewhere, focuses it; the arrow keys move across the groups,
// Enter opens, Escape closes the list and a second Escape clears the text.
import { Fragment, useEffect, useId, useMemo, useRef, useState, type KeyboardEvent, type MouseEvent } from "react";
import { useNavigate } from "react-router";
import { MIN_REMOTE_TEXT, useEventMatches, useFindingMatches } from "../api/commandSearch";
import type { FeedEvent } from "../api/events";
import type { Finding } from "../api/model";
import { SEARCH_KIND_LABEL, useSearchItems } from "../api/search";
import { useCurrentSystem } from "../api/systems";
import { serverNow } from "../lib/clock";
import { COMMAND_KINDS, KIND_PREFIX, narrowedTo, parseCommand, withoutPrefix, type CommandKind, type CommandQuery } from "../lib/commandQuery";
import {
  GROUP_LABEL,
  groupResults,
  moreLabel,
  NARROWED_REMOTE_CAP,
  REMOTE_CAP,
  type RemoteResult,
  type ResultGroup,
  type ResultRow,
} from "../lib/commandResults";
import { withGlobal } from "../lib/globalQuery";
import { useDebounced } from "../lib/useDebounced";
import { useGlobalQuery } from "../lib/useGlobalQuery";
import { useTimeRange } from "../lib/useTimeRange";

const MAC = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

/** Quiet time after a keystroke before findings and events are asked for. */
export const SEARCH_DEBOUNCE_MS = 200;

const ROW_KIND_LABEL: Record<CommandKind, string> = { ...SEARCH_KIND_LABEL, finding: "Finding", event: "Event" };

/** Cmd+K on a Mac, where Ctrl+K kills the line; Ctrl+K elsewhere. */
export function isSearchShortcut(e: Pick<globalThis.KeyboardEvent, "key" | "metaKey" | "ctrlKey">, mac = MAC): boolean {
  return e.key.toLowerCase() === "k" && (mac ? e.metaKey && !e.ctrlKey : e.ctrlKey && !e.metaKey);
}

/** A field that takes typing keeps its keys. */
function isTextField(el: Element | null): boolean {
  if (!(el instanceof HTMLElement)) return false;
  return el.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(el.tagName);
}

/** Whether the text asks the server for findings or events: a prefix of that kind, or enough text with no prefix. */
export function wantsRemote(q: CommandQuery, kind: "finding" | "event"): boolean {
  if (q.kind !== undefined) return q.kind === kind;
  return q.text.length >= MIN_REMOTE_TEXT;
}

type Option = { id: string; row: ResultRow; group?: undefined } | { id: string; row?: undefined; group: ResultGroup };

interface RemoteQuery<T> {
  isPending: boolean;
  isPlaceholderData: boolean;
  error: unknown;
  data?: { items: T[]; totalCount: number; nextPageToken: string };
}

/**
 * What a findings or events group shows. While the text settles or the
 * next answer loads, the rows of the previous text stand in, marked
 * stale: the query's own data while the text settles (only when the
 * narrowing is the same), and its placeholder after, which the query keeps
 * only for the same system, range and narrowing.
 */
function remote<T>(wanted: boolean, settling: boolean, sameNarrowing: boolean, r: RemoteQuery<T>): RemoteResult<T> {
  if (!wanted) return { state: "idle", items: [], total: 0 };
  const data = r.data;
  if ((settling && sameNarrowing && data) || (!settling && r.isPlaceholderData && data)) {
    return { state: "loading", items: data.items, total: data.totalCount, stale: true };
  }
  if (settling || r.isPending) return { state: "loading", items: [], total: 0 };
  if (r.error) return { state: "error", items: [], total: 0 };
  return { state: "ready", items: data?.items ?? [], total: data?.totalCount ?? 0, hasMore: (data?.nextPageToken ?? "") !== "" };
}

export function SearchJump() {
  const { spec } = useTimeRange();
  const global = useGlobalQuery();
  const navigate = useNavigate();
  const [text, setText] = useState("");
  const [open, setOpen] = useState(false);
  const [used, setUsed] = useState(false);
  const [active, setActive] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const listId = useId();
  const names = useSearchItems(spec, used);
  const known = useCurrentSystem().known === true;
  const query = useMemo(() => parseCommand(text), [text]);
  const settledText = useDebounced(text, SEARCH_DEBOUNCE_MS);
  const settled = useMemo(() => parseCommand(settledText), [settledText]);
  const settling = settledText !== text;
  const ask = {
    text: settled.text,
    sinceMs: settled.sinceMs,
    limit: settled.kind === undefined ? REMOTE_CAP : NARROWED_REMOTE_CAP,
  };
  const findingsQ = useFindingMatches(spec, ask, used && wantsRemote(settled, "finding"));
  const eventsQ = useEventMatches(spec, ask, used && wantsRemote(settled, "event"));
  const groups = groupResults(
    query,
    names.data ?? [],
    remote<Finding>(known && wantsRemote(query, "finding"), settling, settled.kind === query.kind, findingsQ),
    remote<FeedEvent>(known && wantsRemote(query, "event"), settling, settled.kind === query.kind, eventsQ),
    serverNow(),
  );
  const options: Option[] = [];
  for (const g of groups) {
    for (const row of g.rows) options.push({ id: `${listId}-${options.length}`, row });
    if (g.more) options.push({ id: `${listId}-${options.length}`, group: g });
  }
  const current = Math.min(active, Math.max(0, options.length - 1));

  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (!isSearchShortcut(e)) return;
      if (document.activeElement !== input.current && isTextField(document.activeElement)) return;
      e.preventDefault();
      input.current?.focus();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  useEffect(() => setActive(0), [text]);

  const close = () => {
    setText("");
    setOpen(false);
    input.current?.blur();
  };
  const choose = (o: Option) => {
    if (o.row) {
      navigate(withGlobal(o.row.path, global));
      close();
    } else if (o.group.more?.kind === "narrow") {
      setText(narrowedTo(text, o.group.more.to));
      setOpen(true);
      input.current?.focus();
    } else if (o.group.more?.kind === "page") {
      navigate(withGlobal(o.group.more.path, global));
      close();
    }
  };

  const showList = open && text.trim() !== "";
  const showHint = open && text.trim() === "";
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown" && options.length) {
      e.preventDefault();
      setOpen(true);
      setActive((current + 1) % options.length);
    } else if (e.key === "ArrowUp" && options.length) {
      e.preventDefault();
      setActive((current - 1 + options.length) % options.length);
    } else if (e.key === "Enter" && showList && options[current]) {
      e.preventDefault();
      choose(options[current]);
    } else if (e.key === "Escape") {
      e.preventDefault();
      if (open) setOpen(false);
      else setText("");
    }
  };

  const chips: { label: string; remove: "kind" | "since" }[] = [];
  if (query.kind) chips.push({ label: GROUP_LABEL[query.kind], remove: "kind" });
  if (query.sinceLabel) chips.push({ label: `Since ${query.sinceLabel}`, remove: "since" });

  const optionProps = (o: Option, extra = "") => {
    const on = o === options[current];
    return {
      id: o.id,
      role: "option",
      "aria-selected": on,
      className: [extra, on ? "on" : ""].filter(Boolean).join(" ") || undefined,
      onMouseDown: (e: MouseEvent) => {
        e.preventDefault();
        choose(o);
      },
      onMouseEnter: () => setActive(options.indexOf(o)),
    };
  };
  const rowOption = (row: ResultRow) => options.find((o) => o.row === row)!;
  const moreOption = (g: ResultGroup) => options.find((o) => o.group === g)!;

  return (
    <div className="search">
      <svg viewBox="0 0 14 14" aria-hidden="true">
        <circle cx="6" cy="6" r="4.5" fill="none" stroke="currentColor" strokeWidth="1.5" />
        <path d="M9.5 9.5 13 13" stroke="currentColor" strokeWidth="1.5" />
      </svg>
      <input
        ref={input}
        type="search"
        role="combobox"
        aria-label="Search this system"
        aria-autocomplete="list"
        aria-expanded={showList && options.length > 0}
        aria-controls={showList ? listId : undefined}
        aria-activedescendant={showList && options[current] ? options[current].id : undefined}
        placeholder="Search this system"
        value={text}
        onChange={(e) => {
          setText(e.target.value);
          setOpen(true);
        }}
        onFocus={() => {
          setUsed(true);
          setOpen(true);
        }}
        onBlur={() => setOpen(false)}
        onKeyDown={onKey}
      />
      <kbd aria-hidden="true">{MAC ? "⌘K" : "Ctrl K"}</kbd>
      {showHint && (
        <div className="search-pop card">
          <p className="search-note search-hint">
            Names, check codes and event types. Narrow with{" "}
            {COMMAND_KINDS.map((k) => (
              <Fragment key={k}>
                <code>{KIND_PREFIX[k]}:</code>{" "}
              </Fragment>
            ))}
            <code>since:2h</code>
          </p>
        </div>
      )}
      {showList && (
        <div className="search-pop card">
          {chips.length > 0 && (
            <div className="search-chips">
              {chips.map((c) => (
                <span className="search-chip" key={c.remove}>
                  {c.label}
                  <button
                    type="button"
                    aria-label={`Remove ${c.label.toLowerCase()}`}
                    tabIndex={-1}
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => setText(withoutPrefix(text, c.remove))}
                  >
                    ×
                  </button>
                </span>
              ))}
            </div>
          )}
          <ul role="listbox" id={listId} aria-label="Results">
            {groups.map((g) => (
              <li role="presentation" key={g.kind}>
                <div className="search-group" id={`${listId}-g-${g.kind}`}>
                  <span>{g.label}</span>
                  {g.state === "loading" && <span className="search-state">Searching…</span>}
                  {g.state === "error" && <span className="search-state">Could not search {g.label.toLowerCase()}</span>}
                </div>
                <ul role="group" aria-labelledby={`${listId}-g-${g.kind}`} aria-busy={g.stale ? true : undefined} className={g.stale ? "search-stale" : undefined}>
                  {g.rows.map((r) => (
                    <li key={r.key} {...optionProps(rowOption(r), r.kind === "finding" ? "search-finding" : "")}>
                      <span className="sr-only">{ROW_KIND_LABEL[r.kind]}</span>
                      <span className="sn">
                        {r.severity && <i className={`search-sev ${r.severity}`} aria-hidden="true" />}
                        <span className="mono">{r.name}</span>
                        {r.detail && <span className="sd">{r.detail}</span>}
                      </span>
                      {r.note && <span className="ss mono">{r.note}</span>}
                    </li>
                  ))}
                  {g.more && <li {...optionProps(moreOption(g), "search-more")}>{moreLabel(g)}</li>}
                </ul>
              </li>
            ))}
          </ul>
          {groups.length === 0 && (
            <p className="search-note" role="status">
              {names.isPending ? "Loading names…" : names.error ? "Could not load names." : "No match."}
            </p>
          )}
        </div>
      )}
    </div>
  );
}

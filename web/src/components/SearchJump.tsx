// Search in the header: type part of a name, pick a result, and its page
// opens. Ctrl+K or Cmd+K focuses it; the arrow keys move, Enter opens and
// Escape closes.
import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { useLocation, useNavigate } from "react-router";
import { SEARCH_KIND_LABEL, searchItems, useSearchItems, type SearchItem } from "../api/search";
import { withGlobal } from "../lib/globalQuery";
import { useGlobalQuery } from "../lib/useGlobalQuery";
import { useTimeRange } from "../lib/useTimeRange";

const MAC = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

export function SearchJump() {
  const { spec } = useTimeRange();
  const global = useGlobalQuery();
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const [text, setText] = useState("");
  const [open, setOpen] = useState(false);
  const [used, setUsed] = useState(false);
  const [active, setActive] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const listId = useId();
  const q = useSearchItems(spec, used);
  const results = useMemo(() => searchItems(q.data ?? [], text), [q.data, text]);

  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        input.current?.focus();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  useEffect(() => setActive(0), [text]);

  const go = (r: SearchItem) => {
    navigate(withGlobal(r.path.startsWith("?") ? `${pathname}${r.path}` : r.path, global));
    setText("");
    setOpen(false);
    input.current?.blur();
  };

  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown" && results.length) {
      e.preventDefault();
      setOpen(true);
      setActive((a) => (a + 1) % results.length);
    } else if (e.key === "ArrowUp" && results.length) {
      e.preventDefault();
      setActive((a) => (a - 1 + results.length) % results.length);
    } else if (e.key === "Enter" && results[active]) {
      e.preventDefault();
      go(results[active]);
    } else if (e.key === "Escape") {
      e.preventDefault();
      if (text) setText("");
      else input.current?.blur();
      setOpen(false);
    }
  };

  const showList = open && text.trim() !== "";
  const optId = (i: number) => `${listId}-${i}`;
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
        aria-expanded={showList && results.length > 0}
        aria-controls={listId}
        aria-activedescendant={showList && results[active] ? optId(active) : undefined}
        placeholder="Search servers, streams, consumers"
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
      {showList && (
        <div className="search-pop card">
          <ul role="listbox" id={listId} aria-label="Results">
            {results.map((r, i) => (
              <li
                key={`${r.kind}:${r.path}`}
                id={optId(i)}
                role="option"
                aria-selected={i === active}
                className={i === active ? "on" : undefined}
                onMouseDown={(e) => {
                  e.preventDefault();
                  go(r);
                }}
                onMouseEnter={() => setActive(i)}
              >
                <span className="sk">{SEARCH_KIND_LABEL[r.kind]}</span>
                <span className="sn mono">{r.name}</span>
                {r.sub && <span className="ss mono">{r.sub}</span>}
              </li>
            ))}
          </ul>
          {results.length === 0 && (
            <p className="search-note" role="status">
              {q.isPending ? "Loading names…" : q.error ? "Could not load names." : "No match."}
            </p>
          )}
        </div>
      )}
    </div>
  );
}

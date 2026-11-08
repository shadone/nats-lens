import {useId,useRef,useState,type ReactNode} from "react";
import {ChartReadout} from "../charts/ChartReadout";

/** An (i) button that shows a description on hover, focus or click. */
export function InfoTip({ label, text }: { label: string; text: ReactNode }) {
  // Hover shows it while the pointer stays; a click pins it until the next
  // click, blur or Escape.
  const [hover, setHover] = useState(false);
  const [pinned, setPinned] = useState(false);
  const open = hover || pinned;
  const id = useId();
  const anchor=useRef<HTMLSpanElement>(null);
  const close = () => {
    setHover(false);
    setPinned(false);
  };
  return (
    <span ref={anchor} className="infotip" onMouseEnter={() => setHover(true)} onMouseLeave={() => setHover(false)}>
      <button
        type="button"
        className="infotip-b"
        aria-label={`About ${label}`}
        aria-expanded={open}
        aria-describedby={open ? id : undefined}
        onClick={() => pinned ? close() : setPinned(true)}
        onFocus={() => setHover(true)}
        onBlur={close}
        onKeyDown={(e) => e.key === "Escape" && close()}
      >
        i
      </button>
      {open && (
        <ChartReadout anchor={anchor} fraction={.5} id={id} contentKey={label}>{text}</ChartReadout>
      )}
    </span>
  );
}


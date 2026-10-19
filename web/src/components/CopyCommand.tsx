// A shell command to run by hand, with a button that copies it.
import { useEffect, useState } from "react";

type CopyState = "idle" | "copied" | "failed";

/** Copies text to the clipboard; false when the browser refuses. */
async function copy(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

/** A command is undefined when a name it needs holds a control character. */
export function CopyCommand({ label, command, button = "Copy", note }: { label: string; command: string | undefined; button?: string; note?: string }) {
  const [state, setState] = useState<CopyState>("idle");
  useEffect(() => setState("idle"), [command]);
  if (command === undefined) {
    return (
      <div className="copy-cmd">
        <div className="lbl">{label}</div>
        <p className="copy-none">No command: the name holds a control character, such as a line break, that a shell would misread.</p>
      </div>
    );
  }
  return (
    <div className="copy-cmd">
      <div className="lbl">{label}</div>
      <div className="copy-row">
        <code className="mono">{command}</code>
        <button type="button" className="btn" onClick={async () => setState((await copy(command)) ? "copied" : "failed")}>
          {button}
        </button>
      </div>
      {note && <p className="copy-note">{note}</p>}
      <span className={`copy-st${state === "failed" ? " bad" : ""}`} role="status" aria-live="polite">
        {state === "copied" ? "Copied." : state === "failed" ? "The browser did not allow copying; select the command instead." : ""}
      </span>
    </div>
  );
}

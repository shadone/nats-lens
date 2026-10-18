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

export function CopyCommand({ label, command, button = "Copy" }: { label: string; command: string; button?: string }) {
  const [state, setState] = useState<CopyState>("idle");
  useEffect(() => setState("idle"), [command]);
  return (
    <div className="copy-cmd">
      <div className="lbl">{label}</div>
      <div className="copy-row">
        <code className="mono">{command}</code>
        <button type="button" className="btn" onClick={async () => setState((await copy(command)) ? "copied" : "failed")}>
          {button}
        </button>
      </div>
      <span className={`copy-st${state === "failed" ? " bad" : ""}`} role="status" aria-live="polite">
        {state === "copied" ? "Copied." : state === "failed" ? "The browser did not allow copying; select the command instead." : ""}
      </span>
    </div>
  );
}
